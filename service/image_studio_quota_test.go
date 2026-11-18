package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageStudioFreeQuotaReservationsAreSharedAndIdempotent(t *testing.T) {
	server := miniredis.RunT(t)
	originalRedis, originalRDB := common.RedisEnabled, common.RDB
	common.OptionMapRWMutex.Lock()
	originalLimit := common.OptionMap[ImageStudioDailyFreeImageKey]
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[ImageStudioDailyFreeImageKey] = "10"
	common.OptionMapRWMutex.Unlock()
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = originalRDB
		common.RedisEnabled = originalRedis
		common.OptionMapRWMutex.Lock()
		if optionMapWasNil {
			common.OptionMap = nil
		} else {
			common.OptionMap[ImageStudioDailyFreeImageKey] = originalLimit
		}
		common.OptionMapRWMutex.Unlock()
	})

	_, err := ReserveImageStudioFreeImages(0)
	require.ErrorIs(t, err, ErrImageStudioInvalidImageCount)
	first, err := ReserveImageStudioFreeImages(4)
	require.NoError(t, err)
	second, err := ReserveImageStudioFreeImages(4)
	require.NoError(t, err)
	_, err = ReserveImageStudioFreeImages(3)
	require.ErrorIs(t, err, ErrImageStudioQuotaExceeded)
	require.NoError(t, ResizeImageStudioFreeImageReservation(first, 6))
	status, err := GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Equal(t, 10, status.Used)
	require.NoError(t, ResizeImageStudioFreeImageReservation(first, 4))
	status, err = GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Equal(t, 8, status.Used)
	assert.ErrorIs(t, ResizeImageStudioFreeImageReservation(first, 7), ErrImageStudioQuotaExceeded)

	ReleaseImageStudioFreeImages(first)
	ReleaseImageStudioFreeImages(first)
	status, err = GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Equal(t, 4, status.Used)

	SettleImageStudioFreeImages(second, 3)
	SettleImageStudioFreeImages(second, 3)
	status, err = GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Equal(t, 3, status.Used)
}

func TestImageStudioFreeQuotaResetInvalidatesOldReservations(t *testing.T) {
	server := miniredis.RunT(t)
	originalRedis, originalRDB := common.RedisEnabled, common.RDB
	common.OptionMapRWMutex.Lock()
	originalLimit := common.OptionMap[ImageStudioDailyFreeImageKey]
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[ImageStudioDailyFreeImageKey] = "10"
	common.OptionMapRWMutex.Unlock()
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = originalRDB
		common.RedisEnabled = originalRedis
		common.OptionMapRWMutex.Lock()
		if optionMapWasNil {
			common.OptionMap = nil
		} else {
			common.OptionMap[ImageStudioDailyFreeImageKey] = originalLimit
		}
		common.OptionMapRWMutex.Unlock()
	})

	oldReservation, err := ReserveImageStudioFreeImages(8)
	require.NoError(t, err)
	require.NoError(t, ResetImageStudioDailyFreeQuota())
	newReservation, err := ReserveImageStudioFreeImages(5)
	require.NoError(t, err)
	ReleaseImageStudioFreeImages(oldReservation)

	status, err := GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Equal(t, 5, status.Used)
	ReleaseImageStudioFreeImages(newReservation)
	status, err = GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Zero(t, status.Used)
}

func TestFinalizeImageStudioTaskQuotaPersistsTerminalSettlement(t *testing.T) {
	t.Cleanup(func() {
		model.DB.Where("task_id IN ?", []string{"image-studio-quota-success", "image-studio-quota-failure"}).Delete(&model.Task{})
	})
	server := miniredis.RunT(t)
	originalRedis, originalRDB := common.RedisEnabled, common.RDB
	common.OptionMapRWMutex.Lock()
	originalLimit := common.OptionMap[ImageStudioDailyFreeImageKey]
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[ImageStudioDailyFreeImageKey] = "10"
	common.OptionMapRWMutex.Unlock()
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = originalRDB
		common.RedisEnabled = originalRedis
		common.OptionMapRWMutex.Lock()
		if optionMapWasNil {
			common.OptionMap = nil
		} else {
			common.OptionMap[ImageStudioDailyFreeImageKey] = originalLimit
		}
		common.OptionMapRWMutex.Unlock()
	})

	successReservation, err := ReserveImageStudioFreeImages(6)
	require.NoError(t, err)
	successTask := &model.Task{TaskID: "image-studio-quota-success", Status: model.TaskStatusSuccess}
	successTask.PrivateData.ImageStudioQuotaReservation = successReservation
	require.NoError(t, successTask.Insert())
	FinalizeImageStudioTaskQuota(successTask, 4)
	assert.Nil(t, successTask.PrivateData.ImageStudioQuotaReservation)
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, successTask.ID).Error)
	assert.Nil(t, persisted.PrivateData.ImageStudioQuotaReservation)

	failureReservation, err := ReserveImageStudioFreeImages(3)
	require.NoError(t, err)
	failureTask := &model.Task{TaskID: "image-studio-quota-failure", Status: model.TaskStatusFailure}
	failureTask.PrivateData.ImageStudioQuotaReservation = failureReservation
	require.NoError(t, failureTask.Insert())
	FinalizeImageStudioTaskQuota(failureTask, 2)
	status, err := GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Equal(t, 4, status.Used)
	persisted = model.Task{}
	require.NoError(t, model.DB.First(&persisted, failureTask.ID).Error)
	assert.Nil(t, persisted.PrivateData.ImageStudioQuotaReservation)
}
