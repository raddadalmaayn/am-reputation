package main

import (
  "encoding/json"
  "errors"
  "fmt"
  "math"
  "strings"
  "time"

  "github.com/hyperledger/fabric-contract-api-go/contractapi"
  m "github.com/raddad/am-reputation/chaincode/internal"
)

const (
  stateKeyPrefix = "rep:"   // rep:<actorId>:<dim>
  defaultLambda  = 0.99999  // decay base per second (tune later)
)

func makeKey(actorID, dim string) string {
  return fmt.Sprintf("%s%s:%s", stateKeyPrefix, strings.ToLower(actorID), strings.ToLower(dim))
}

// SubmitRating(targetID, dim, outcome(0|1), cid, ts)
// NOTE: rater weight/stake/VC checks will be added in the next step.
func (rc *ReputationContract) SubmitRating(ctx contractapi.TransactionContextInterface,
  targetID string, dim string, outcome int, cid string, ts int64) (float64, error) {

  if targetID == "" || dim == "" {
    return 0, errors.New("targetID and dim are required")
  }
  if outcome != 0 && outcome != 1 {
    return 0, errors.New("outcome must be 0 or 1")
  }
  if ts <= 0 {
    ts = time.Now().Unix()
  }

  key := makeKey(targetID, dim)
  stub := ctx.GetStub()

  var st m.RepState
  b, err := stub.GetState(key)
  if err != nil {
    return 0, fmt.Errorf("get state: %w", err)
  }
  if b != nil {
    if err := json.Unmarshal(b, &st); err != nil {
      return 0, fmt.Errorf("unmarshal: %w", err)
    }
  } else {
    st = m.RepState{ActorID: targetID, Dim: dim, Alpha: 0, Beta: 0, LastTs: ts}
  }

  // Time decay
  if st.LastTs > 0 && ts > st.LastTs {
    dt := float64(ts - st.LastTs)
    factor := math.Pow(defaultLambda, dt)
    st.Alpha *= factor
    st.Beta  *= factor
  }

  // (temp) rater weight = 1.0; we add bounded weight + stake next
  if outcome == 1 {
    st.Alpha += 1.0
  } else {
    st.Beta  += 1.0
  }
  st.LastTs = ts

  out, err := json.Marshal(&st)
  if err != nil {
    return 0, fmt.Errorf("marshal: %w", err)
  }
  if err := stub.PutState(key, out); err != nil {
    return 0, fmt.Errorf("put state: %w", err)
  }

  // Emit lightweight event for off-chain listeners (keeps on-chain small)
  evt := fmt.Sprintf("%s|%s|%d|%s|%d", targetID, dim, outcome, cid, ts)
  _ = stub.SetEvent("RatingAppended", []byte(evt))

  return st.Score(), nil
}

func (rc *ReputationContract) GetReputation(ctx contractapi.TransactionContextInterface,
  targetID string, dim string) (*m.RepState, error) {

  key := makeKey(targetID, dim)
  b, err := ctx.GetStub().GetState(key)
  if err != nil {
    return nil, fmt.Errorf("get state: %w", err)
  }
  if b == nil {
    st := &m.RepState{ActorID: targetID, Dim: dim, Alpha: 0, Beta: 0, LastTs: 0}
    return st, nil
  }
  var st m.RepState
  if err := json.Unmarshal(b, &st); err != nil {
    return nil, fmt.Errorf("unmarshal: %w", err)
  }
  return &st, nil
}
