package testplatform

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Interpret 把平台结果翻译成可直接落库的事实。
//
// 设计取舍：平台的 summary 字段随门类与引擎版本变化，这里按"候选键依次尝试"的
// 宽容策略取值，取不到就留空（页面显示未提供），**绝不编造数值**。
// 数据是否"完整"由 DataComplete 表达，供 Q1 的提交门禁使用；
// 是否"通过"只看 verdict，供登记接入门禁使用。
func Interpret(category Category, state JobState, reportJSON []byte) Outcome {
	out := Outcome{Verdict: state.Verdict}
	passed := state.Verdict == "pass" || state.Verdict == "pass_with_warnings"

	summary := decodeObject(state.Summary)
	document := decodeObject(reportJSON)
	// 报告的 summary 可能嵌在文档里（{"summary":{...}}），也可能平铺在根上，两种都试。
	nested, _ := lookup(document, "summary").(map[string]any)

	switch category {
	case CategoryBaseline:
		out.ConnectivityPassed = passed
		out.ConnectionMS = firstDecimal(summary, nested, document,
			"connection_ms", "minimal_chat_ms", "first_token_ms", "ttft_ms", "latency_ms",
			"l1.minimal_chat.ms", "layers.l1.minimal_chat_ms")
		out.DataComplete = passed && out.ConnectionMS != nil
	case CategoryStress:
		out.PressurePassed = passed
		out.RPS = firstDecimal(summary, nested, document, "qps", "rps", "steady_qps", "throughput_qps")
		out.TPS = firstDecimal(summary, nested, document, "tps", "steady_tps", "throughput_tps")
		out.DataComplete = passed && out.RPS != nil && out.TPS != nil
	default:
		out.DataComplete = passed
	}
	out.CacheHitTokens, out.CacheInputTokens = firstTokens(summary, nested, document)
	out.CacheRatioPercent = firstRatio(summary, nested, document,
		"cache_hit_ratio_pct", "cache_hit_ratio", "cache.hit_ratio_pct")
	out.CacheRatioProvided = out.CacheRatioPercent != nil
	if scene := firstString(summary, nested, document, "scene", "cache_scene", "cache.scene"); scene != "" {
		out.Scene = scene
	}
	return out
}

// firstRatio 取缓存命中率百分比。只认 0-100 的数值，超范围视为脏数据丢弃。
func firstRatio(sources ...any) *string {
	var objects []map[string]any
	var keys []string
	for _, source := range sources {
		if object, ok := source.(map[string]any); ok {
			objects = append(objects, object)
			continue
		}
		if key, ok := source.(string); ok {
			keys = append(keys, key)
		}
	}
	for _, object := range objects {
		for _, key := range keys {
			value, ok := numericString(lookup(object, key))
			if !ok {
				continue
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 || parsed > 100 {
				continue
			}
			return &value
		}
	}
	return nil
}

// decodeObject 宽容解码：非法 JSON 或非对象一律返回空 map，调用方无需判空。
func decodeObject(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return object
}

// lookup 按点分路径取值，支持 map[string]any 逐层下钻。
func lookup(object map[string]any, path string) any {
	if object == nil {
		return nil
	}
	current := any(object)
	for _, segment := range strings.Split(path, ".") {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		next, exists := asMap[segment]
		if !exists {
			return nil
		}
		current = next
	}
	return current
}

// firstDecimal 返回第一个能解析成非负小数的候选值。
func firstDecimal(sources ...any) *string {
	var objects []map[string]any
	var keys []string
	for _, source := range sources {
		if object, ok := source.(map[string]any); ok {
			objects = append(objects, object)
			continue
		}
		if key, ok := source.(string); ok {
			keys = append(keys, key)
		}
	}
	for _, object := range objects {
		for _, key := range keys {
			if value, ok := numericString(lookup(object, key)); ok {
				return &value
			}
		}
	}
	return nil
}

// firstString 返回第一个非空字符串候选值。
func firstString(sources ...any) string {
	var objects []map[string]any
	var keys []string
	for _, source := range sources {
		if object, ok := source.(map[string]any); ok {
			objects = append(objects, object)
			continue
		}
		if key, ok := source.(string); ok {
			keys = append(keys, key)
		}
	}
	for _, object := range objects {
		for _, key := range keys {
			if raw, exists := lookup(object, key).(string); exists && strings.TrimSpace(raw) != "" {
				return strings.TrimSpace(raw)
			}
		}
	}
	return ""
}

// firstTokens 提取缓存命中/输入 token 计数；两者必须同时存在且自洽，否则都留空。
func firstTokens(sources ...map[string]any) (*int64, *int64) {
	for _, object := range sources {
		hit, okHit := integerValue(lookup(object, "cache_hit_tokens"))
		input, okInput := integerValue(lookup(object, "cache_input_tokens"))
		if !okHit {
			hit, okHit = integerValue(lookup(object, "cache_hit_prompt_tokens"))
		}
		if !okInput {
			input, okInput = integerValue(lookup(object, "total_prompt_tokens"))
		}
		if !okHit || !okInput {
			hit, okHit = integerValue(lookup(object, "cache.hit_tokens"))
			input, okInput = integerValue(lookup(object, "cache.input_tokens"))
		}
		if okHit && okInput && input > 0 && hit >= 0 && hit <= input {
			return &hit, &input
		}
	}
	return nil, nil
}

// numericString 把 JSON 数字或数字字符串规范成十进制字符串。
func numericString(value any) (string, bool) {
	switch typed := value.(type) {
	case float64:
		formatted := FormatDecimal(typed)
		if formatted == nil {
			return "", false
		}
		return *formatted, true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return "", false
		}
		formatted := FormatDecimal(parsed)
		if formatted == nil {
			return "", false
		}
		return *formatted, true
	case string:
		trimmed := strings.TrimSpace(typed)
		if !decimalPattern.MatchString(trimmed) {
			return "", false
		}
		return trimmed, true
	}
	return "", false
}

// integerValue 把 JSON 数字或数字字符串解析成整数。
func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}
