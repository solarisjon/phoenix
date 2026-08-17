package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON Schemas for every call whose output Phoenix parses as JSON. Set on
// provider.TaskRequest.ResponseSchema; adapters that can constrain decoding
// (llama.cpp json_schema/grammar, Ollama format, OpenAI response_format)
// enforce them, others ignore them and callers fall back to tolerant parsing
// plus a single RepairStructured pass (repair.go).
//
// Schemas are kept deliberately simple (objects, strings, numbers, enums,
// arrays; every property required; additionalProperties=false) so they are
// accepted by strict OpenAI mode and compile to GBNF on llama.cpp.

// PlanSchema is the orchestrator's decomposition plan (see routedPlan).
var PlanSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "confidence": {"type": "number", "minimum": 0, "maximum": 1},
    "rationale": {"type": "string"},
    "subtasks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "title": {"type": "string"},
          "description": {"type": "string"},
          "domain": {"type": "string", "enum": ["code","write","analyse","research","ops","design","test","other"]},
          "complexity": {"type": "string", "enum": ["low","medium","high"]},
          "agent_id": {"type": ["string","null"]},
          "provider_id": {"type": ["string","null"]},
          "model_id": {"type": ["string","null"]}
        },
        "required": ["title","description","domain","complexity","agent_id","provider_id","model_id"],
        "additionalProperties": false
      }
    }
  },
  "required": ["confidence","rationale","subtasks"],
  "additionalProperties": false
}`)

// HealthSchema is the monitor health classifier's output.
var HealthSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "signal": {"type": "string", "enum": ["all_clear","needs_attention","failed"]},
    "reason": {"type": "string"}
  },
  "required": ["signal","reason"],
  "additionalProperties": false
}`)

// AgentGenSchema is the "generate an agent from a description" result.
var AgentGenSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "behaviour": {"type": "string"},
    "guardrails": {"type": "string"},
    "hard_guardrails": {"type": "string"},
    "persona": {"type": "string"},
    "instructions": {"type": "string"}
  },
  "required": ["behaviour","guardrails","hard_guardrails","persona","instructions"],
  "additionalProperties": false
}`)

// SuggestionsSchema wraps next-action suggestions in an object (schema roots
// must be objects for OpenAI strict mode; llama.cpp doesn't care).
var SuggestionsSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "suggestions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {"title": {"type": "string"}, "description": {"type": "string"}},
        "required": ["title","description"],
        "additionalProperties": false
      }
    }
  },
  "required": ["suggestions"],
  "additionalProperties": false
}`)

// VaultPickSchema constrains the Obsidian vault choice to the known names.
func VaultPickSchema(names []string) json.RawMessage {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		b, _ := json.Marshal(n)
		quoted = append(quoted, string(b))
	}
	return json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "properties": {"vault": {"type": "string", "enum": [%s]}},
  "required": ["vault"],
  "additionalProperties": false
}`, strings.Join(quoted, ",")))
}

// TextFieldSchema is a one-field object {"<field>": string} for description /
// guardrail generation, so constrained backends return exactly one string.
func TextFieldSchema(field string) json.RawMessage {
	b, _ := json.Marshal(field)
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{%s:{"type":"string"}},"required":[%s],"additionalProperties":false}`, b, b))
}
