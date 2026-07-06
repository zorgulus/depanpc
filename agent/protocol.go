package main

import "encoding/json"

// Types de messages échangés sur le WebSocket.
const (
	MsgAuth                 = "auth"
	MsgRequest              = "request"
	MsgResponse             = "response"
	MsgConfirmationRequired = "confirmation_required"
	MsgConfirm              = "confirm"
	MsgError                = "error"
)

// Message est l'enveloppe JSON unique pour tous les échanges.
// Selon le type, seuls certains champs sont pertinents (voir docs/PROTOCOL.md).
type Message struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Command      string          `json:"command,omitempty"`
	Params       json.RawMessage `json:"params,omitempty"`
	Status       string          `json:"status,omitempty"`
	Result       interface{}     `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
	ConfirmToken string          `json:"confirm_token,omitempty"`
	Token        string          `json:"token,omitempty"`
}
