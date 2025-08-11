package internal

type ReputationState struct {
  ActorID string  `json:"actorId"`
  Dim     string  `json:"dim"`
  Alpha   float64 `json:"alpha"`
  Beta    float64 `json:"beta"`
  LastTs  int64   `json:"lastTs"`
}
