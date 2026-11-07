package session

import (
	"encoding/json"

	"github.com/Lowpower/pigo/internal/ai"
)

// VirtualModelStateType is the custom entry that stores router state on a branch.
// The shape matches pi v1.3.0: data is {provider, modelId, state}.
const VirtualModelStateType = "pi.virtual-model-state"

// BranchSelection is the model a session branch should resume.
// A virtual model_change holds across later physical replies while that model
// is still registered. Otherwise the latest successful physical reply wins.
// Entries with no selection return ok false, and the caller keeps its current model.
func BranchSelection(entries []Entry, virtual func(provider, id string) bool) (provider, modelID string, ok bool) {
	if virtual == nil {
		virtual = func(string, string) bool { return false }
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type == "model_change" {
			if e.Provider == "" || e.ModelID == "" {
				continue
			}
			return e.Provider, e.ModelID, true
		}
		msg, okMsg := assistantSelection(e)
		if !okMsg {
			continue
		}
		if change, found := lastModelChange(entries, i); found && virtual(change.Provider, change.ModelID) {
			return change.Provider, change.ModelID, true
		}
		return msg.Provider, msg.Model, true
	}
	return "", "", false
}

// BranchThinkingLevel is the latest thinking_level_change on the branch.
func BranchThinkingLevel(entries []Entry) (string, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type != "thinking_level_change" {
			continue
		}
		if entries[i].ThinkingLevel == "" {
			continue
		}
		return entries[i].ThinkingLevel, true
	}
	return "", false
}

// VirtualModelState is the latest router state stored on the branch for one virtual model.
func VirtualModelState(entries []Entry, provider, modelID string) (map[string]any, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type != "custom" || e.CustomType != VirtualModelStateType || len(e.Data) == 0 {
			continue
		}
		var data struct {
			Provider string         `json:"provider"`
			ModelID  string         `json:"modelId"`
			State    map[string]any `json:"state"`
		}
		if json.Unmarshal(e.Data, &data) != nil {
			continue
		}
		if data.Provider != provider || data.ModelID != modelID || data.State == nil {
			continue
		}
		return data.State, true
	}
	return nil, false
}

type assistantIdentity struct {
	Provider string
	Model    string
}

func assistantSelection(e Entry) (assistantIdentity, bool) {
	if e.Type != "message" && e.Type != "" {
		return assistantIdentity{}, false
	}
	var msg struct {
		Role       string `json:"role"`
		API        string `json:"api"`
		Provider   string `json:"provider"`
		Model      string `json:"model"`
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(e.Message, &msg) != nil || msg.Role != "assistant" {
		return assistantIdentity{}, false
	}
	if msg.API == ai.VirtualModelAPI {
		return assistantIdentity{}, false
	}
	if msg.StopReason == string(ai.StopError) || msg.StopReason == string(ai.StopAborted) {
		return assistantIdentity{}, false
	}
	if msg.Provider == "" || msg.Model == "" {
		return assistantIdentity{}, false
	}
	return assistantIdentity{Provider: msg.Provider, Model: msg.Model}, true
}

func lastModelChange(entries []Entry, before int) (Entry, bool) {
	for i := before - 1; i >= 0; i-- {
		if entries[i].Type != "model_change" {
			continue
		}
		if entries[i].Provider == "" || entries[i].ModelID == "" {
			continue
		}
		return entries[i], true
	}
	return Entry{}, false
}
