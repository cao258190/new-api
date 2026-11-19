package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const responseAliasContextKey = "response_start_continuation"

// ResponseAliasPrefix reserves gateway IDs whose upstream identity is not yet
// known when a response.created event is sent. Aliases are scoped to a user and
// API token, and remain resolvable for 30 days after the last response event.
const ResponseAliasPrefix = "resp_na_start_"

// ResponseStartItemPrefix marks empty gateway messages. They are discarded
// when replayed as input because no upstream account owns these item IDs.
const ResponseStartItemPrefix = "msg_na_start_"

type responseAlias struct {
	Upstream string `json:"upstream"`
	Model    string `json:"model"`
	Group    string `json:"group"`
	Channel  int    `json:"channel"`
	KeyIndex int    `json:"key_index"`
	KeyHash  string `json:"key_hash"`
	Origin   string `json:"origin"`
}

var responseAliases struct {
	sync.Once
	cache *cachex.HybridCache[string]
}

func responseAliasCache() *cachex.HybridCache[string] {
	// The Redis client can be reinitialized without discarding the local cache.
	// A Redis error stays an error; silently falling back would lose aliases on
	// another node after an already committed response.created event.
	if common.RedisEnabled && common.RDB != nil {
		return cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace("response_start_alias"), Redis: common.RDB, RedisCodec: cachex.StringCodec{},
		})
	}
	responseAliases.Do(func() {
		responseAliases.cache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace("response_start_alias"),
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, 100_000).WithTTL(30 * 24 * time.Hour).WithJanitor().Build()
			},
		})
	})
	return responseAliases.cache
}

func responseAliasCacheKey(c *gin.Context, alias string) string {
	return fmt.Sprintf("%d:%d:%s", c.GetInt("id"), c.GetInt("token_id"), alias)
}

// RememberResponseAlias persists only routing identity, never the credential or
// response body. A failed write must stop publication of the aliased response.
func RememberResponseAlias(c *gin.Context, info *relaycommon.RelayInfo, alias, upstream string) error {
	if upstream == "" || len(upstream) > 512 || info.ChannelMeta == nil || c.GetInt("id") <= 0 {
		return errors.New("invalid upstream response identity")
	}
	group := info.UsingGroup
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	entry := responseAlias{Upstream: upstream, Model: info.OriginModelName, Group: group,
		Channel: info.ChannelId, KeyIndex: info.ChannelMultiKeyIndex,
		KeyHash: fmt.Sprintf("%x", sha256.Sum256([]byte(info.ApiKey))),
		Origin:  info.ChannelBaseUrl}
	raw, err := common.Marshal(entry)
	if err != nil {
		return err
	}
	if err = responseAliasCache().SetWithTTL(responseAliasCacheKey(c, alias), string(raw), 30*24*time.Hour); err != nil {
		return errors.New(i18n.T(c, i18n.MsgRelayResponseStorageUnavailable))
	}
	return nil
}

// ResolveResponseAlias restores previous_response_id before parsing and channel
// selection, even after the plugin is disabled. Normal model access checks still
// run; the alias cannot grant access to a different user, token, group or model.
func ResolveResponseAlias(c *gin.Context, modelName string) (bool, *types.NewAPIError) {
	if c.Request.URL.Path != "/v1/responses" && c.Request.URL.Path != "/v1/responses/compact" {
		return false, nil
	}
	if _, done := c.Get(responseAliasContextKey); done {
		return false, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return false, nil
	} // The regular request parser owns body errors.
	body, err := storage.Bytes()
	if err != nil {
		return false, nil
	}
	changed := false
	input := gjson.GetBytes(body, "input")
	if input.IsArray() {
		items := input.Array()
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if !strings.HasPrefix(item.Get("id").Str, ResponseStartItemPrefix) {
				continue
			}
			empty := item.Get("type").Str == "item_reference"
			if item.Get("type").Str == "message" && item.Get("role").Str == "assistant" {
				content := item.Get("content")
				empty = content.IsArray() && len(content.Array()) == 0
			}
			if !empty {
				continue
			}
			body, err = sjson.DeleteBytes(body, fmt.Sprintf("input.%d", i))
			if err != nil {
				return false, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
			}
			changed = true
		}
	}
	if changed {
		replacement, replaceErr := common.CreateBodyStorage(body)
		if replaceErr != nil {
			return false, types.NewError(replaceErr, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		_ = storage.Close()
		storage = replacement
		c.Set(common.KeyBodyStorage, replacement)
		c.Set(common.KeyRequestBody, nil)
		c.Request.Body = io.NopCloser(replacement)
		c.Request.ContentLength = int64(len(body))
	}
	id := gjson.GetBytes(body, "previous_response_id").Str
	if !strings.HasPrefix(id, ResponseAliasPrefix) {
		return changed, nil
	}
	invalid := func(status int, messageKey string) (bool, *types.NewAPIError) {
		return false, types.NewErrorWithStatusCode(errors.New(i18n.T(c, messageKey)), types.ErrorCodeInvalidRequest, status, types.ErrOptionWithSkipRetry())
	}
	if len(id) > 128 || c.GetInt("id") <= 0 {
		return invalid(http.StatusBadRequest, i18n.MsgRelayResponseUnavailable)
	}
	raw, found, err := responseAliasCache().Get(responseAliasCacheKey(c, id))
	if err != nil {
		return invalid(http.StatusServiceUnavailable, i18n.MsgRelayResponseStorageUnavailable)
	}
	var entry responseAlias
	if !found || common.UnmarshalJsonStr(raw, &entry) != nil || entry.Upstream == "" || entry.Model != modelName {
		return invalid(http.StatusBadRequest, i18n.MsgRelayResponseUnavailable)
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		if !slices.Contains(GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup)), entry.Group) {
			return invalid(http.StatusBadRequest, i18n.MsgRelayResponseUnavailable)
		}
	} else if group != entry.Group {
		return invalid(http.StatusBadRequest, i18n.MsgRelayResponseUnavailable)
	}
	if !model.IsChannelEnabledForGroupModel(entry.Group, modelName, entry.Channel) {
		return invalid(http.StatusBadRequest, i18n.MsgRelayResponseUnavailable)
	}
	constraints := GetChannelConstraints(c)
	if pin, exists, _ := constraints.ResolvedPin(); exists && pin.ChannelId != entry.Channel {
		return invalid(http.StatusBadRequest, i18n.MsgRelayResponseChannelConflict)
	}
	patched, err := sjson.SetBytes(body, "previous_response_id", entry.Upstream)
	if err != nil {
		return invalid(http.StatusBadRequest, i18n.MsgRelayResponseUnavailable)
	}
	replacement, err := common.CreateBodyStorage(patched)
	if err != nil {
		return invalid(http.StatusInternalServerError, i18n.MsgRelayResponseStorageUnavailable)
	}
	_ = storage.Close()
	c.Set(common.KeyBodyStorage, replacement)
	c.Set(common.KeyRequestBody, nil)
	c.Request.Body = io.NopCloser(replacement)
	c.Request.ContentLength = int64(len(patched))
	c.Set(responseAliasContextKey, entry)
	constraints.AddPin(dto.ChannelPin{ChannelId: entry.Channel, Source: "response_continuation", Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySingleAttempt})
	if group == "auto" {
		common.SetContextKey(c, constant.ContextKeyAutoGroup, entry.Group)
	}
	return true, nil
}

// ResponseContinuationKey reuses the original active credential on a continuation;
// a key rotation or disabled key fails closed instead of contacting another account.
func ResponseContinuationKey(c *gin.Context, channel *model.Channel) (string, int, *types.NewAPIError) {
	value, exists := c.Get(responseAliasContextKey)
	if !exists {
		return channel.GetNextEnabledKey()
	}
	entry, ok := value.(responseAlias)
	key := channel.Key
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		if entry.KeyIndex < 0 || entry.KeyIndex >= len(keys) {
			ok = false
		} else {
			key = keys[entry.KeyIndex]
		}
		status := channel.ChannelInfo.MultiKeyStatusList[entry.KeyIndex]
		ok = ok && (status == 0 || status == common.ChannelStatusEnabled)
	}
	if !ok || channel.Id != entry.Channel || channel.GetBaseURL() != entry.Origin || fmt.Sprintf("%x", sha256.Sum256([]byte(key))) != entry.KeyHash {
		return "", 0, types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgRelayResponseUnavailable)), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return key, entry.KeyIndex, nil
}

// CheckResponseContinuationConnection prevents a continuation from crossing
// upstream accounts on an already established Responses WebSocket session.
func CheckResponseContinuationConnection(c *gin.Context, channel *model.Channel, key string) *types.NewAPIError {
	if _, exists := c.Get(responseAliasContextKey); !exists {
		return nil
	}
	expected, _, apiErr := ResponseContinuationKey(c, channel)
	if apiErr != nil {
		return apiErr
	}
	if expected != key {
		return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgRelayResponseConnectionRequired)), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return nil
}
