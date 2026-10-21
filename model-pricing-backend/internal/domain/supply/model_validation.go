package supply

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

func modelTypeKnown(value string) bool {
	return value == "TEXT" || value == "IMAGE" || value == "VIDEO" || value == "AUDIO"
}

func validateFacts(path string, facts []OfficialFact) error {
	seen := map[string]bool{}
	for i, fact := range facts {
		field := fmt.Sprintf("%s[%d]", path, i)
		if !stableKey.MatchString(fact.Key) || seen[fact.Key] {
			return invalid(field+".key", "expected a unique stable key")
		}
		seen[fact.Key] = true
		switch fact.Disclosure {
		case "DISCLOSED":
			if strings.TrimSpace(fact.Value) == "" {
				return invalid(field+".value", "disclosed fact requires a value")
			}
		case "UNDISCLOSED", "UNVERIFIED":
		default:
			return invalid(field+".disclosure", "unsupported disclosure state")
		}
	}
	return nil
}

func validateModelData(data ModelData, now time.Time) error {
	if data.ModelType != "" && !modelTypeKnown(data.ModelType) {
		return invalid("model_type", "unsupported model type")
	}
	for _, entry := range []struct {
		key    string
		values []string
	}{{"input_modalities", data.InputModalities}, {"output_modalities", data.OutputModalities}} {
		seen := map[string]bool{}
		for _, value := range entry.values {
			if !modelTypeKnown(value) || seen[value] {
				return invalid(entry.key, "expected distinct known modalities")
			}
			seen[value] = true
		}
	}
	for _, entry := range []struct {
		key    string
		values []OfficialFact
	}{{"capacity", data.Capacity}, {"api_access", data.APIAccess}, {"lifecycle", data.Lifecycle}} {
		if err := validateFacts(entry.key, entry.values); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i, ability := range data.Abilities {
		field := fmt.Sprintf("abilities[%d]", i)
		if !stableKey.MatchString(ability.Key) || seen[ability.Key] {
			return invalid(field+".key", "expected a unique stable key")
		}
		seen[ability.Key] = true
		switch ability.State {
		case "SUPPORTED", "UNSUPPORTED", "UNDISCLOSED", "UNVERIFIED":
		default:
			return invalid(field+".state", "unsupported ability state")
		}
	}
	if err := ValidatePriceItems(data.PriceItems); err != nil {
		return err
	}
	for i, source := range data.Sources {
		field := fmt.Sprintf("sources[%d]", i)
		if strings.TrimSpace(source.Label) == "" {
			return invalid(field+".label", "required")
		}
		if !publicURL(source.URL) {
			return invalid(field+".url", "expected HTTP(S) source without credentials")
		}
		switch source.Kind {
		case "OFFICIAL_SITE", "OFFICIAL_DOCS", "OFFICIAL_PRICING":
		default:
			return invalid(field+".source_kind", "unsupported source kind")
		}
	}
	if data.CheckedAt != "" {
		date, err := time.Parse("2006-01-02", data.CheckedAt)
		if err != nil || date.Format("2006-01-02") != data.CheckedAt {
			return invalid("checked_at", "expected a real YYYY-MM-DD date")
		}
		shanghai, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return fmt.Errorf("load display timezone: %w", err)
		}
		if data.CheckedAt > now.In(shanghai).Format("2006-01-02") {
			return invalid("checked_at", "verification date is in the future")
		}
	}
	return nil
}

func (m *StandardModel) MissingFields() []string {
	d := m.Data
	missing := []string{}
	for _, value := range []struct{ key, value string }{{"company", d.Company}, {"name", d.Name}, {"official_id", d.OfficialID}, {"model_type", d.ModelType}, {"checked_at", d.CheckedAt}} {
		if strings.TrimSpace(value.value) == "" {
			missing = append(missing, value.key)
		}
	}
	if len(d.InputModalities) == 0 {
		missing = append(missing, "input_modalities")
	}
	if len(d.OutputModalities) == 0 {
		missing = append(missing, "output_modalities")
	}
	for _, entry := range []struct {
		key    string
		values []OfficialFact
	}{{"capacity", d.Capacity}, {"api_access", d.APIAccess}, {"lifecycle", d.Lifecycle}} {
		if len(entry.values) == 0 {
			missing = append(missing, entry.key)
		}
		for i, value := range entry.values {
			if value.Disclosure == "UNVERIFIED" {
				missing = append(missing, fmt.Sprintf("%s[%d]", entry.key, i))
			}
		}
	}
	if len(d.Abilities) == 0 {
		missing = append(missing, "abilities")
	}
	for i, value := range d.Abilities {
		if value.State == "UNVERIFIED" {
			missing = append(missing, fmt.Sprintf("abilities[%d]", i))
		}
	}
	if len(d.Sources) == 0 {
		missing = append(missing, "sources")
	}
	tasks := map[string]bool{}
	for _, price := range d.PriceItems {
		tasks[price.TaskType] = true
	}
	if len(d.PriceItems) == 0 {
		missing = append(missing, "price_items")
	} else {
		switch d.ModelType {
		case "TEXT":
			for _, task := range []string{"TEXT_INPUT", "TEXT_OUTPUT"} {
				if !tasks[task] {
					missing = append(missing, "price_items."+task)
				}
			}
		case "IMAGE":
			if !tasks["IMAGE_OUTPUT"] && !tasks["IMAGE_GENERATION"] {
				missing = append(missing, "price_items.IMAGE_OUTPUT_OR_GENERATION")
			}
		case "VIDEO":
			if !tasks["VIDEO_OUTPUT"] && !tasks["VIDEO_GENERATION"] {
				missing = append(missing, "price_items.VIDEO_OUTPUT_OR_GENERATION")
			}
		case "AUDIO":
			if !tasks["AUDIO_OUTPUT"] && !tasks["AUDIO_TRANSCRIPTION"] && !tasks["AUDIO_RECOGNITION"] {
				missing = append(missing, "price_items.AUDIO_TASK")
			}
		}
	}
	slices.Sort(missing)
	return missing
}
