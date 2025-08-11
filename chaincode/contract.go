package main

import (
  "encoding/json"
  "errors"
  "fmt"
  "math"
  "strconv"
  "strings"

  "github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
  m "github.com/raddadalmaayn/am-reputation/chaincode/internal"
)

const (
  stateKeyPrefix = "rep:"   // rep:<actorId>:<dim>
  defaultLambda  = 0.99999  // decay base per second (tune later)
)

func makeKey(actorID, dim string) string {
  return fmt.Sprintf("%s%s:%s", stateKeyPrefix, strings.ToLower(actorID), strings.ToLower(dim))
}

func (rc *ReputationContract) GetReputation(ctx contractapi.TransactionContextInterface, actorID, dim string) (*m.ReputationState, error) {
  key := makeKey(actorID, dim)
  b, err := ctx.GetStub().GetState(key)
  if err != nil {
    return nil, err
  }
  if len(b) == 0 {
    // not found -> empty state
    return &m.ReputationState{ActorID: actorID, Dim: dim, Alpha: 0, Beta: 0, LastTs: 0}, nil
  }
  var st m.ReputationState
  if err := json.Unmarshal(b, &st); err != nil {
    return nil, err
  }
  return &st, nil
}

// SubmitRating(targetID, dim, outcome(0|1), cid, ts)
func (rc *ReputationContract) SubmitRating(ctx contractapi.TransactionContextInterface,
  targetID, dim, outcomeStr, cid string, tsStr string) (int, error) {

  // Parse inputs
  outcomeInt, err := strconv.Atoi(outcomeStr)
  if err != nil || (outcomeInt != 0 && outcomeInt != 1) {
    return 0, errors.New("outcome must be '0' or '1'")
  }
  ts, err := strconv.ParseInt(tsStr, 10, 64)
  if err != nil || ts <= 0 {
    return 0, errors.New("invalid ts")
  }

  // Load current state
  key := makeKey(targetID, dim)
  b, err := ctx.GetStub().GetState(key)
  if err != nil {
    return 0, err
  }
  st := m.ReputationState{ActorID: targetID, Dim: dim, Alpha: 0, Beta: 0, LastTs: 0}
  if len(b) != 0 {
    if err := json.Unmarshal(b, &st); err != nil {
      return 0, err
    }
  }

  // Time-decay (only forward in time)
  if st.LastTs > 0 && ts > st.LastTs {
    dt := ts - st.LastTs
    decay := math.Pow(defaultLambda, float64(dt))
    st.Alpha *= decay
    st.Beta *= decay
  }

  // Binary update
  if outcomeInt == 1 {
    st.Alpha += 1.0
  } else {
    st.Beta += 1.0
  }
  st.LastTs = ts

  // Persist
  out, err := json.Marshal(&st)
  if err != nil {
    return 0, err
  }
  if err := ctx.GetStub().PutState(key, out); err != nil {
    return 0, err
  }

  // For now we just return 1 to signal success
  return 1, nil
}
