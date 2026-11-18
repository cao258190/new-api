package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/go-redis/redis/v8"
)

const (
	ImageStudioDailyFreeImageLimit = 150
	ImageStudioDailyFreeImageMax   = 1_000_000
	ImageStudioDailyFreeImageKey   = "image_studio.daily_free_image_limit"
	ImageStudioFreeImageGroup      = "free-image"
)

var (
	ErrImageStudioQuotaUnavailable  = errors.New("image studio free quota storage is unavailable")
	ErrImageStudioQuotaExceeded     = errors.New("daily free image quota is insufficient")
	ErrImageStudioInvalidImageCount = errors.New("image count is invalid")
)

type ImageStudioQuotaStatus struct {
	DailyLimit int       `json:"daily_limit"`
	Used       int       `json:"used"`
	Remaining  int       `json:"remaining"`
	ResetsAt   time.Time `json:"resets_at"`
}

var imageStudioReserveScript = redis.NewScript(`
local generation = redis.call('GET', KEYS[1])
if not generation then
  generation = '0'
  redis.call('SET', KEYS[1], generation)
  redis.call('EXPIREAT', KEYS[1], ARGV[6])
end
local counterKey = ARGV[1] .. generation
local used = tonumber(redis.call('GET', counterKey) or '0')
local ledgerGeneration = redis.call('HGET', KEYS[2], 'generation')
if ledgerGeneration and ledgerGeneration ~= generation then
  return {-1, used, generation}
end
if redis.call('HGET', KEYS[2], 'finalized') == '1' then
  return {-1, used, generation}
end
local count = tonumber(ARGV[3])
local limit = tonumber(ARGV[4])
local previousTotal = tonumber(redis.call('HGET', KEYS[2], 'reserved_total') or '0')
local delta = count - previousTotal
if delta < 0 then
  return {1, used, generation}
end
if count < 1 or used + delta > limit then
  return {0, used, generation}
end
local nextUsed = used
if delta > 0 then
  nextUsed = redis.call('INCRBY', counterKey, delta)
end
redis.call('EXPIREAT', counterKey, ARGV[5])
redis.call('HSET', KEYS[2], 'day', ARGV[2], 'generation', generation, 'remaining', count, 'reserved_total', count, 'finalized', '0')
redis.call('EXPIREAT', KEYS[2], ARGV[5])
redis.call('EXPIREAT', KEYS[1], ARGV[6])
return {1, nextUsed, generation}
`)

var imageStudioStatusScript = redis.NewScript(`
local generation = redis.call('GET', KEYS[1]) or '0'
local used = tonumber(redis.call('GET', ARGV[1] .. generation) or '0')
return {used, generation}
`)

var imageStudioFinalizeScript = redis.NewScript(`
local generation = redis.call('GET', KEYS[1]) or '0'
if redis.call('HGET', KEYS[2], 'finalized') == '1' then
  return 0
end
local counterKey = ARGV[2] .. generation
if generation ~= ARGV[1] or redis.call('HGET', KEYS[2], 'generation') ~= generation then
  redis.call('HSET', KEYS[2], 'finalized', '1', 'remaining', '0')
  return 0
end
local remaining = tonumber(redis.call('HGET', KEYS[2], 'remaining') or '0')
local keep = tonumber(ARGV[3])
local nextRemaining = math.min(remaining, math.max(0, keep))
local release = remaining - nextRemaining
local used = tonumber(redis.call('GET', counterKey) or '0')
local nextUsed = math.max(0, used - release)
redis.call('SET', counterKey, nextUsed)
redis.call('EXPIREAT', counterKey, ARGV[4])
redis.call('HSET', KEYS[2], 'remaining', nextRemaining, 'reserved_total', nextRemaining, 'finalized', '1')
redis.call('EXPIREAT', KEYS[2], ARGV[4])
redis.call('EXPIREAT', KEYS[1], ARGV[5])
return nextRemaining
`)

var imageStudioShrinkScript = redis.NewScript(`
local generation = redis.call('GET', KEYS[1]) or '0'
if redis.call('HGET', KEYS[2], 'finalized') == '1' or generation ~= ARGV[1] or redis.call('HGET', KEYS[2], 'generation') ~= generation then
  return -1
end
local counterKey = ARGV[2] .. generation
local remaining = tonumber(redis.call('HGET', KEYS[2], 'remaining') or '0')
local keep = math.min(remaining, tonumber(ARGV[3]))
local release = remaining - keep
local used = tonumber(redis.call('GET', counterKey) or '0')
local nextUsed = math.max(0, used - release)
redis.call('SET', counterKey, nextUsed)
redis.call('EXPIREAT', counterKey, ARGV[4])
redis.call('HSET', KEYS[2], 'remaining', keep, 'reserved_total', keep)
redis.call('EXPIREAT', KEYS[2], ARGV[4])
return keep
`)

var imageStudioResetScript = redis.NewScript(`
local generation = redis.call('INCR', KEYS[1])
redis.call('EXPIREAT', KEYS[1], ARGV[1])
return generation
`)

func imageStudioDay(now time.Time) (string, time.Time) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	localNow := now.In(location)
	reset := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, location)
	return localNow.Format("20060102"), reset
}

func imageStudioResetAt(day string) (time.Time, error) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	date, err := time.ParseInLocation("20060102", day, location)
	if err != nil {
		return time.Time{}, err
	}
	return date.AddDate(0, 0, 1), nil
}

func imageStudioKeys(day string) (string, string) {
	base := "image_studio:{free_image}:" + day
	return base + ":generation", base + ":used:"
}

func ImageStudioDailyFreeImageLimitValue() int {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[ImageStudioDailyFreeImageKey]
	common.OptionMapRWMutex.RUnlock()
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 || limit > ImageStudioDailyFreeImageMax {
		return ImageStudioDailyFreeImageLimit
	}
	return limit
}

func GetImageStudioQuotaStatus() (ImageStudioQuotaStatus, error) {
	limit := ImageStudioDailyFreeImageLimitValue()
	day, resetAt := imageStudioDay(time.Now())
	generationKey, counterPrefix := imageStudioKeys(day)
	if !common.RedisEnabled || common.RDB == nil {
		return ImageStudioQuotaStatus{}, ErrImageStudioQuotaUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := imageStudioStatusScript.Run(ctx, common.RDB, []string{generationKey}, counterPrefix).Int64Slice()
	if err != nil {
		return ImageStudioQuotaStatus{}, fmt.Errorf("read image studio free quota: %w", err)
	}
	if len(result) != 2 {
		return ImageStudioQuotaStatus{}, errors.New("invalid image studio quota status response")
	}
	used := int(result[0])
	remaining := max(limit-used, 0)
	return ImageStudioQuotaStatus{DailyLimit: limit, Used: used, Remaining: remaining, ResetsAt: resetAt}, nil
}

func ReserveImageStudioFreeImages(count int) (*relaycommon.ImageStudioQuotaReservation, error) {
	if count < 1 || count > ImageStudioDailyFreeImageMax {
		return nil, ErrImageStudioInvalidImageCount
	}
	if !common.RedisEnabled || common.RDB == nil {
		return nil, ErrImageStudioQuotaUnavailable
	}

	day, resetAt := imageStudioDay(time.Now())
	generationKey, counterPrefix := imageStudioKeys(day)
	reservationID, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return nil, fmt.Errorf("create image studio quota reservation: %w", err)
	}
	limit := ImageStudioDailyFreeImageLimitValue()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ledgerKey := "image_studio:{free_image}:reservation:" + reservationID
	result, err := imageStudioReserveScript.Run(ctx, common.RDB, []string{generationKey, ledgerKey}, counterPrefix, day, count, limit, resetAt.Add(24*time.Hour).Unix(), resetAt.Add(24*time.Hour).Unix()).Int64Slice()
	if err != nil {
		return nil, fmt.Errorf("reserve image studio free quota: %w", err)
	}
	if len(result) != 3 {
		return nil, errors.New("invalid image studio quota reservation response")
	}
	if result[0] != 1 {
		if result[0] < 0 {
			return nil, ErrImageStudioQuotaExceeded
		}
		return nil, ErrImageStudioQuotaExceeded
	}
	return &relaycommon.ImageStudioQuotaReservation{Day: day, Generation: strconv.FormatInt(result[2], 10), ID: reservationID, Images: count}, nil
}

func ResizeImageStudioFreeImageReservation(reservation *relaycommon.ImageStudioQuotaReservation, count int) error {
	if reservation == nil || count < 1 || count > ImageStudioDailyFreeImageMax {
		return ErrImageStudioInvalidImageCount
	}
	if count == reservation.Images {
		return nil
	}
	if count > reservation.Images {
		if err := resizeImageStudioReservation(reservation, count); err != nil {
			return err
		}
		reservation.Images = count
		return nil
	}
	if err := shrinkImageStudioReservation(reservation, count); err != nil {
		return err
	}
	reservation.Images = count
	return nil
}

func resizeImageStudioReservation(reservation *relaycommon.ImageStudioQuotaReservation, count int) error {
	if !common.RedisEnabled || common.RDB == nil {
		return ErrImageStudioQuotaUnavailable
	}
	generationKey, counterPrefix := imageStudioKeys(reservation.Day)
	resetAt, err := imageStudioResetAt(reservation.Day)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := imageStudioReserveScript.Run(ctx, common.RDB,
		[]string{generationKey, "image_studio:{free_image}:reservation:" + reservation.ID},
		counterPrefix, reservation.Day, count, ImageStudioDailyFreeImageLimitValue(),
		resetAt.Add(24*time.Hour).Unix(), resetAt.Add(24*time.Hour).Unix()).Int64Slice()
	if err != nil {
		return fmt.Errorf("grow image studio free quota reservation: %w", err)
	}
	if len(result) != 3 || result[0] != 1 || strconv.FormatInt(result[2], 10) != reservation.Generation {
		return ErrImageStudioQuotaExceeded
	}
	return nil
}

func finalizeImageStudioReservation(reservation *relaycommon.ImageStudioQuotaReservation, keep int) error {
	if reservation == nil || reservation.ID == "" || !common.RedisEnabled || common.RDB == nil {
		return ErrImageStudioQuotaUnavailable
	}
	resetAt, err := imageStudioResetAt(reservation.Day)
	if err != nil {
		return err
	}
	generationKey, counterPrefix := imageStudioKeys(reservation.Day)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return imageStudioFinalizeScript.Run(ctx, common.RDB,
		[]string{generationKey, "image_studio:{free_image}:reservation:" + reservation.ID},
		reservation.Generation, counterPrefix, keep,
		resetAt.Add(24*time.Hour).Unix(), resetAt.Add(24*time.Hour).Unix()).Err()
}

func shrinkImageStudioReservation(reservation *relaycommon.ImageStudioQuotaReservation, keep int) error {
	if reservation == nil || reservation.ID == "" || !common.RedisEnabled || common.RDB == nil {
		return ErrImageStudioQuotaUnavailable
	}
	resetAt, err := imageStudioResetAt(reservation.Day)
	if err != nil {
		return err
	}
	generationKey, counterPrefix := imageStudioKeys(reservation.Day)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := imageStudioShrinkScript.Run(ctx, common.RDB,
		[]string{generationKey, "image_studio:{free_image}:reservation:" + reservation.ID},
		reservation.Generation, counterPrefix, keep,
		resetAt.Add(24*time.Hour).Unix()).Int64()
	if err != nil {
		return fmt.Errorf("shrink image studio free quota reservation: %w", err)
	}
	if result < 0 {
		return ErrImageStudioQuotaExceeded
	}
	return nil
}

func ReleaseImageStudioFreeImages(reservation *relaycommon.ImageStudioQuotaReservation) error {
	if reservation == nil || reservation.Images <= 0 || reservation.ID == "" || !common.RedisEnabled || common.RDB == nil {
		if reservation == nil || reservation.Images <= 0 || reservation.ID == "" {
			return nil
		}
		return ErrImageStudioQuotaUnavailable
	}
	if err := finalizeImageStudioReservation(reservation, 0); err != nil {
		common.SysError(common.LogText("failed to release image studio free quota reservation: %s", err.Error()))
		return err
	} else {
		reservation.Images = 0
	}
	return nil
}

func SettleImageStudioFreeImages(reservation *relaycommon.ImageStudioQuotaReservation, actualCount int) error {
	if reservation == nil {
		return nil
	}
	if actualCount < 1 || actualCount > reservation.Images {
		actualCount = reservation.Images
	}
	if err := finalizeImageStudioReservation(reservation, actualCount); err != nil {
		common.SysError(common.LogText("failed to settle image studio free quota reservation: %s", err.Error()))
		return err
	}
	reservation.Images = actualCount
	return nil
}

func FinalizeImageStudioTaskQuota(task *model.Task, actualCount int) {
	if task == nil || task.PrivateData.ImageStudioQuotaReservation == nil {
		return
	}
	reservation := task.PrivateData.ImageStudioQuotaReservation
	if task.Status == model.TaskStatusFailure {
		actualCount = 0
	} else if actualCount <= 0 || actualCount > reservation.Images {
		actualCount = reservation.Images
	}
	if err := finalizeImageStudioReservation(reservation, actualCount); err != nil {
		common.SysError(common.LogText("failed to finalize Image Studio free quota for task %s: %s", task.TaskID, err.Error()))
		return
	}
	reservation.Images = actualCount
	task.PrivateData.ImageStudioQuotaReservation = nil
	if err := task.Update(); err != nil {
		task.PrivateData.ImageStudioQuotaReservation = reservation
		common.SysError(common.LogText("failed to persist Image Studio free quota settlement for task %s: %s", task.TaskID, err.Error()))
	}
}

func ImageStudioImageCountFromUsageFacts(facts map[string]any) int {
	value, ok := facts["image_count"]
	if !ok {
		return 0
	}
	switch count := value.(type) {
	case int:
		if count >= 1 && count <= ImageStudioDailyFreeImageMax {
			return count
		}
	case int64:
		if count >= 1 && count <= ImageStudioDailyFreeImageMax {
			return int(count)
		}
	case uint:
		if count >= 1 && count <= ImageStudioDailyFreeImageMax {
			return int(count)
		}
	case float64:
		if count >= 1 && count <= ImageStudioDailyFreeImageMax && math.Trunc(count) == count {
			return int(count)
		}
	case float32:
		if count >= 1 && count <= ImageStudioDailyFreeImageMax && math.Trunc(float64(count)) == float64(count) {
			return int(count)
		}
	case string:
		parsed, err := strconv.Atoi(count)
		if err == nil && parsed >= 1 && parsed <= ImageStudioDailyFreeImageMax {
			return parsed
		}
	}
	return 0
}

func ResetImageStudioDailyFreeQuota() error {
	if !common.RedisEnabled || common.RDB == nil {
		return ErrImageStudioQuotaUnavailable
	}
	day, resetAt := imageStudioDay(time.Now())
	generationKey, _ := imageStudioKeys(day)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := imageStudioResetScript.Run(ctx, common.RDB, []string{generationKey}, resetAt.Add(24*time.Hour).Unix()).Err(); err != nil {
		return fmt.Errorf("reset image studio free quota: %w", err)
	}
	return nil
}
