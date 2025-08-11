package internal

type RepState struct {
  ActorID string  `json:"actorId"`
  Dim     string  `json:"dim"`
  Alpha   float64 `json:"alpha"`
  Beta    float64 `json:"beta"`
  LastTs  int64   `json:"lastTs"`
}

func (rs *RepState) Score() float64 {
  denom := rs.Alpha + rs.Beta
  if denom <= 0 {
    return 0.0
  }
  return rs.Alpha / denom
}
