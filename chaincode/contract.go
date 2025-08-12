package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// ---- Types ----

// Reputation aggregates successes/failures per (actor, dimension).
// (alpha, beta) are counts; lastTs is the most recent accepted rating timestamp.
type Reputation struct {
	ActorID string `json:"actorId"`
	Dim     string `json:"dim"`
	Alpha   int64  `json:"alpha"`  // success count
	Beta    int64  `json:"beta"`   // failure count
	LastTs  int64  `json:"lastTs"` // last accepted rating ts
}

// Rating is stored per unique (actor, dim, ts, cid) so SubmitRating is idempotent.
type Rating struct {
	ActorID string `json:"actorId"`
	Dim     string `json:"dim"`
	CID     string `json:"cid"`
	Ts      int64  `json:"ts"`
	Value   int    `json:"value"` // >0 => success; <=0 => failure
}

// ---- Contract ----

type ReputationContract struct {
	contractapi.Contract
}

// ---- Keys ----

const (
	objectTypeReputation = "rep"
	objectTypeRating     = "rat"
)

func repKey(ctx contractapi.TransactionContextInterface, actor, dim string) (string, error) {
	// Composite key: rep~actor~dim
	return ctx.GetStub().CreateCompositeKey(objectTypeReputation, []string{actor, dim})
}

func ratingKey(ctx contractapi.TransactionContextInterface, actor, dim, tsStr, cid string) (string, error) {
	// Composite key: rat~actor~dim~ts~cid
	return ctx.GetStub().CreateCompositeKey(objectTypeRating, []string{actor, dim, tsStr, cid})
}

// ---- Helpers ----

func trim(s string) string { return strings.TrimSpace(s) }

func getOrInitReputation(ctx contractapi.TransactionContextInterface, actor, dim string) (*Reputation, error) {
	k, err := repKey(ctx, actor, dim)
	if err != nil {
		return nil, fmt.Errorf("repKey: %w", err)
	}
	raw, err := ctx.GetStub().GetState(k)
	if err != nil {
		return nil, fmt.Errorf("GetState(rep): %w", err)
	}
	if len(raw) == 0 {
		return &Reputation{ActorID: actor, Dim: dim, Alpha: 0, Beta: 0, LastTs: 0}, nil
	}
	var r Reputation
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("unmarshal(rep): %w", err)
	}
	return &r, nil
}

func putJSON(ctx contractapi.TransactionContextInterface, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal(%T): %w", v, err)
	}
	if err := ctx.GetStub().PutState(key, b); err != nil {
		return fmt.Errorf("PutState(%s): %w", key, err)
	}
	return nil
}

// ---- Transactions ----

// SubmitRating(actorId, dim, valueStr, cid, tsStr) -> returns 1 if applied, 0 if duplicate
func (rc *ReputationContract) SubmitRating(
	ctx contractapi.TransactionContextInterface,
	actorId, dim, valueStr, cid, tsStr string,
) (int, error) {
	actorId = trim(actorId)
	dim = trim(dim)
	cid = trim(cid)

	// Validate required args
	if actorId == "" || dim == "" || cid == "" {
		return 0, errors.New("actorId, dim, cid are required")
	}

	// Parse value
	v, err := strconv.Atoi(trim(valueStr))
	if err != nil {
		return 0, fmt.Errorf("invalid value (expect integer): %w", err)
	}

	// Parse ts
	ts, err := strconv.ParseInt(trim(tsStr), 10, 64)
	if err != nil || ts <= 0 {
		return 0, errors.New("invalid ts (expect positive unix seconds)")
	}

	// Idempotency: guard on (actor, dim, ts, cid)
	rKey, err := ratingKey(ctx, actorId, dim, tsStr, cid)
	if err != nil {
		return 0, fmt.Errorf("ratingKey: %w", err)
	}
	if old, err := ctx.GetStub().GetState(rKey); err != nil {
		return 0, fmt.Errorf("GetState(rating): %w", err)
	} else if len(old) != 0 {
		// Duplicate => no-op
		fmt.Printf("SubmitRating duplicate: actor=%s dim=%s ts=%d cid=%s\n", actorId, dim, ts, cid)
		return 0, nil
	}

	// Load / init aggregate
	rep, err := getOrInitReputation(ctx, actorId, dim)
	if err != nil {
		return 0, err
	}

	// Update counts (success if v>0)
	if v > 0 {
		rep.Alpha++
	} else {
		rep.Beta++
	}
	if ts > rep.LastTs {
		rep.LastTs = ts
	}

	// Persist rating first, then aggregate (safe order)
	if err := putJSON(ctx, rKey, &Rating{
		ActorID: actorId,
		Dim:     dim,
		CID:     cid,
		Ts:      ts,
		Value:   v,
	}); err != nil {
		return 0, err
	}

	k, err := repKey(ctx, actorId, dim)
	if err != nil {
		return 0, fmt.Errorf("repKey(save): %w", err)
	}
	if err := putJSON(ctx, k, rep); err != nil {
		return 0, err
	}

	fmt.Printf("SubmitRating applied: actor=%s dim=%s v=%d => alpha=%d beta=%d lastTs=%d\n",
		actorId, dim, v, rep.Alpha, rep.Beta, rep.LastTs)

	return 1, nil
}

// GetReputation(actorId, dim) -> Reputation JSON (zeros if not found)
func (rc *ReputationContract) GetReputation(
	ctx contractapi.TransactionContextInterface,
	actorId, dim string,
) (*Reputation, error) {
	actorId = trim(actorId)
	dim = trim(dim)
	if actorId == "" || dim == "" {
		return nil, errors.New("actorId and dim are required")
	}
	return getOrInitReputation(ctx, actorId, dim)
}
