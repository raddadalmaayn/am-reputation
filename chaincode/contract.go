package main

import (
	"crypto/sha256"
	"encoding/json"
        "encoding/base64" 
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// ============================================================================
// SMART CONTRACT DEFINITION
// ============================================================================
type ReputationContract struct {
	contractapi.Contract
}

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// SystemConfig holds all governable parameters
type SystemConfig struct {
	// Economic Parameters
	MinStakeRequired float64 `json:"minStakeRequired"`
	DisputeCost      float64 `json:"disputeCost"`
	SlashPercentage  float64 `json:"slashPercentage"`

	// Reputation Parameters
	DecayRate    float64 `json:"decayRate"`
	DecayPeriod  float64 `json:"decayPeriod"`
	InitialAlpha float64 `json:"initialAlpha"`
	InitialBeta  float64 `json:"initialBeta"`
	MinRaterWeight float64 `json:"minRaterWeight"`
	MaxRaterWeight float64 `json:"maxRaterWeight"`

	// Dimension Registry
	ValidDimensions map[string]bool   `json:"validDimensions"`
	MetaDimensions  map[string]string `json:"metaDimensions"` // base -> meta mapping

	// Version Control
	Version     int   `json:"version"`
	LastUpdated int64 `json:"lastUpdated"`
}

// Reputation represents the Beta distribution parameters
type Reputation struct {
	ActorID     string  `json:"actorId"`
	Dimension   string  `json:"dimension"`
	Alpha       float64 `json:"alpha"`
	Beta        float64 `json:"beta"`
	TotalEvents int     `json:"totalEvents"`
	LastTs      int64   `json:"lastTs"`
}

// Rating represents a single rating event
type Rating struct {
	RatingID  string  `json:"ratingId"`
	RaterID   string  `json:"raterId"`
	ActorID   string  `json:"actorId"`
	Dimension string  `json:"dimension"`
	Value     float64 `json:"value"`
	Weight    float64 `json:"weight"`
	Evidence  string  `json:"evidence"`
	Timestamp int64   `json:"timestamp"`
	TxID      string  `json:"txId"`
}

// Stake represents an actor's financial commitment
type Stake struct {
	ActorID   string  `json:"actorId"`
	Balance   float64 `json:"balance"`
	Locked    float64 `json:"locked"`
	UpdatedAt int64   `json:"updatedAt"`
}

// Dispute represents a challenge to a rating
type Dispute struct {
	DisputeID       string `json:"disputeId"`
	RatingID        string `json:"ratingId"`
	InitiatorID     string `json:"initiatorId"`
	RaterID         string `json:"raterId"`
	ActorID         string `json:"actorId"`
	Dimension       string `json:"dimension"`
	Reason          string `json:"reason"`
	Status          string `json:"status"` // pending, upheld, overturned
	ArbitratorID    string `json:"arbitratorId"`
	ArbitratorNotes string `json:"arbitratorNotes"`
	CreatedAt       int64  `json:"createdAt"`
	ResolvedAt      int64  `json:"resolvedAt"`
}

// ============================================================================
// GOVERNANCE FUNCTIONS
// ============================================================================

// InitConfig initializes the system configuration with default values
func (rc *ReputationContract) InitConfig(ctx contractapi.TransactionContextInterface) error {
	// Check if config already exists
	existing, err := ctx.GetStub().GetState("SYSTEM_CONFIG")
	if err != nil {
		return fmt.Errorf("failed to read config: %v", err)
	}
	if existing != nil {
		return fmt.Errorf("config already initialized")
	}

	// Allow anyone to initialize if config doesn't exist (bootstrap)
	config := SystemConfig{
		MinStakeRequired: 10000.0,
		DisputeCost:      100.0,
		SlashPercentage:  0.1,

		DecayRate:      0.98,
		DecayPeriod:    86400.0, // 1 day in seconds
		InitialAlpha:   2.0,
		InitialBeta:    2.0,
		MinRaterWeight: 0.1,
		MaxRaterWeight: 5.0,

		ValidDimensions: map[string]bool{
			"quality":    true,
			"delivery":   true,
			"compliance": true,
			"warranty":   true,
		},
		MetaDimensions: map[string]string{
			"quality":    "rating_quality",
			"delivery":   "rating_delivery",
			"compliance": "rating_compliance",
			"warranty":   "rating_warranty",
		},

		Version:     1,
		LastUpdated: time.Now().Unix(),
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	err = ctx.GetStub().PutState("SYSTEM_CONFIG", configJSON)
	if err != nil {
		return fmt.Errorf("failed to store config: %v", err)
	}

	// Emit event
	ctx.GetStub().SetEvent("ConfigInitialized", configJSON)

	return nil
}

// UpdateConfig allows admin to modify system parameters
func (rc *ReputationContract) UpdateConfig(
	ctx contractapi.TransactionContextInterface,
	configJSON string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: admin role required")
	}

	var newConfig SystemConfig
	if err := json.Unmarshal([]byte(configJSON), &newConfig); err != nil {
		return fmt.Errorf("invalid config JSON: %v", err)
	}

	// Validate new config
	if err := validateConfig(&newConfig); err != nil {
		return fmt.Errorf("invalid configuration: %v", err)
	}

	newConfig.Version++
	newConfig.LastUpdated = time.Now().Unix()

	updatedJSON, err := json.Marshal(newConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	err = ctx.GetStub().PutState("SYSTEM_CONFIG", updatedJSON)
	if err != nil {
		return fmt.Errorf("failed to update config: %v", err)
	}

	// Emit event
	ctx.GetStub().SetEvent("ConfigUpdated", updatedJSON)

	return nil
}

// *** FIX 1: ADD UpdateDecayRate function ***
func (rc *ReputationContract) UpdateDecayRate(
	ctx contractapi.TransactionContextInterface,
	newRateStr string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: admin role required")
	}

	newRate, err := strconv.ParseFloat(newRateStr, 64)
	if err != nil || newRate <= 0 || newRate > 1 {
		return fmt.Errorf("invalid decay rate: must be between 0 and 1")
	}

	config, err := getConfig(ctx)
	if err != nil {
		return err
	}

	config.DecayRate = newRate
	config.Version++
	config.LastUpdated = time.Now().Unix()

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	err = ctx.GetStub().PutState("SYSTEM_CONFIG", configJSON)
	if err != nil {
		return fmt.Errorf("failed to update config: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"decayRate": newRate,
		"version":   config.Version,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("DecayRateUpdated", eventJSON)

	return nil
}

// GetConfig retrieves current system configuration
func (rc *ReputationContract) GetConfig(ctx contractapi.TransactionContextInterface) (*SystemConfig, error) {
	return getConfig(ctx)
}

// AddDimension allows admin to add a new reputation dimension
func (rc *ReputationContract) AddDimension(
	ctx contractapi.TransactionContextInterface,
	baseDimension string,
	metaDimension string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: admin role required")
	}

	config, err := getConfig(ctx)
	if err != nil {
		return err
	}

	// Add dimension
	config.ValidDimensions[baseDimension] = true
	config.MetaDimensions[baseDimension] = metaDimension
	config.Version++
	config.LastUpdated = time.Now().Unix()

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	err = ctx.GetStub().PutState("SYSTEM_CONFIG", configJSON)
	if err != nil {
		return fmt.Errorf("failed to update config: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"baseDimension": baseDimension,
		"metaDimension": metaDimension,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("DimensionAdded", eventJSON)

	return nil
}

// ============================================================================
// STAKE MANAGEMENT
// ============================================================================

// AddStake allows an actor to add financial stake
func (rc *ReputationContract) AddStake(
	ctx contractapi.TransactionContextInterface,
	amountStr string,
) error {
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		return fmt.Errorf("invalid amount: must be positive number")
	}

	// *** FIX 2: Use normalized identity for stake key ***
	actorID, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return fmt.Errorf("failed to get actor ID: %v", err)
	}

	// Normalize identity
	normalizedID := normalizeIdentity(actorID)

	// Load or initialize stake
	stake, err := getOrInitStake(ctx, normalizedID)
	if err != nil {
		return err
	}

	// Update balance
	stake.Balance += amount
	stake.UpdatedAt = time.Now().Unix()

	// Store updated stake
	stakeJSON, err := json.Marshal(stake)
	if err != nil {
		return fmt.Errorf("failed to marshal stake: %v", err)
	}

	stakeKey := fmt.Sprintf("STAKE:%s", normalizedID)
	err = ctx.GetStub().PutState(stakeKey, stakeJSON)
	if err != nil {
		return fmt.Errorf("failed to store stake: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"actorId": normalizedID,
		"amount":  amount,
		"balance": stake.Balance,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("StakeAdded", eventJSON)

	return nil
}

// GetStake retrieves an actor's stake information
func (rc *ReputationContract) GetStake(
	ctx contractapi.TransactionContextInterface,
	actorID string,
) (*Stake, error) {
	normalizedID := normalizeIdentity(actorID)
	return getOrInitStake(ctx, normalizedID)
}

// ============================================================================
// RATING SUBMISSION
// ============================================================================

// SubmitRating allows an actor to rate another actor
func (rc *ReputationContract) SubmitRating(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
	valueStr string,
	evidence string,
	timestampStr string,
) (string, error) {
	// Parse inputs
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil || value < 0 || value > 1 {
		return "", fmt.Errorf("invalid value: must be between 0 and 1")
	}

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp: %v", err)
	}

	// Get rater ID
	raterID, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return "", fmt.Errorf("failed to get rater ID: %v", err)
	}

	// *** ADD DEBUG OUTPUT ***
	fmt.Printf("=== SELF-RATING DEBUG ===\n")
	fmt.Printf("Raw raterID from GetID(): %s\n", raterID)
	fmt.Printf("Input actorID parameter: %s\n", actorID)

	// *** FIX 3: Normalize both IDs for comparison ***
	normalizedRaterID := normalizeIdentity(raterID)
	normalizedActorID := normalizeIdentity(actorID)

	// *** ADD DEBUG OUTPUT ***
	fmt.Printf("Normalized raterID: %s\n", normalizedRaterID)
	fmt.Printf("Normalized actorID: %s\n", normalizedActorID)
	fmt.Printf("Are they equal? %v\n", normalizedRaterID == normalizedActorID)
	fmt.Printf("=========================\n")

	// CRITICAL: Prevent self-rating with normalized IDs
	if normalizedRaterID == normalizedActorID {
		return "", fmt.Errorf("self-rating is not allowed: rater %s cannot rate themselves", normalizedRaterID)
	}

	// ... rest of function continues unchanged
	// Validate dimension
	config, err := getConfig(ctx)
	if err != nil {
		return "", err
	}

	if !config.ValidDimensions[dimension] {
		return "", fmt.Errorf("invalid dimension: %s", dimension)
	}

	// *** FIX 4: Check rater has minimum stake using normalized ID ***
	raterStake, err := getOrInitStake(ctx, normalizedRaterID)
	if err != nil {
		return "", fmt.Errorf("failed to get rater stake: %v", err)
	}

	if raterStake.Balance < config.MinStakeRequired {
		return "", fmt.Errorf("insufficient stake: have %f, require %f", raterStake.Balance, config.MinStakeRequired)
	}

	// Calculate rater weight based on METAREPUTATION
	weight, err := rc.calculateRaterWeight(ctx, normalizedRaterID, dimension)
	if err != nil {
		return "", fmt.Errorf("failed to calculate rater weight: %v", err)
	}

	// Generate rating ID
	txID := ctx.GetStub().GetTxID()
	ratingID := generateRatingID(normalizedRaterID, normalizedActorID, dimension, timestamp)

	// Create rating record (store normalized IDs)
	rating := Rating{
		RatingID:  ratingID,
		RaterID:   normalizedRaterID,
		ActorID:   normalizedActorID,
		Dimension: dimension,
		Value:     value,
		Weight:    weight,
		Evidence:  evidence,
		Timestamp: timestamp,
		TxID:      txID,
	}

	// Store rating
	ratingJSON, err := json.Marshal(rating)
	if err != nil {
		return "", fmt.Errorf("failed to marshal rating: %v", err)
	}

	err = ctx.GetStub().PutState(ratingID, ratingJSON)
	if err != nil {
		return "", fmt.Errorf("failed to store rating: %v", err)
	}
// Store rating
ratingJSON, err = json.Marshal(rating)
if err != nil {
    return "", fmt.Errorf("failed to marshal rating: %v", err)
}

err = ctx.GetStub().PutState(ratingID, ratingJSON)
if err != nil {
    return "", fmt.Errorf("failed to store rating: %v", err)
}

// *** STORE THE RATER-ACTOR PAIR RECORD ***
raterActorKey := fmt.Sprintf("RATER_ACTOR:%s:%s:%s", normalizedRaterID, normalizedActorID, dimension)
raterActorRecord := map[string]interface{}{
    "raterId":   normalizedRaterID,
    "actorId":   normalizedActorID,
    "dimension": dimension,
    "ratingId":  ratingID,
    "timestamp": timestamp,
}
raterActorJSON, _ := json.Marshal(raterActorRecord)
ctx.GetStub().PutState(raterActorKey, raterActorJSON)

// Update actor's reputation
err = rc.updateReputation(ctx, &rating)
	// Update actor's reputation
	err = rc.updateReputation(ctx, &rating)
	if err != nil {
		return "", fmt.Errorf("failed to update reputation: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"ratingId":  ratingID,
		"raterId":   normalizedRaterID,
		"actorId":   normalizedActorID,
		"dimension": dimension,
		"value":     value,
		"weight":    weight,
		"timestamp": timestamp,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("RatingSubmitted", eventJSON)

	return ratingID, nil
}

// updateReputation updates the actor's Beta distribution parameters
func (rc *ReputationContract) updateReputation(
	ctx contractapi.TransactionContextInterface,
	rating *Rating,
) error {
	config, err := getConfig(ctx)
	if err != nil {
		return err
	}

	// Load or initialize reputation
	rep, err := getOrInitReputation(ctx, rating.ActorID, rating.Dimension, config)
	if err != nil {
		return err
	}

	// Update Beta parameters with weighted rating
	if rating.Value >= 0.5 {
		rep.Alpha += rating.Weight * rating.Value
	} else {
		rep.Beta += rating.Weight * (1.0 - rating.Value)
	}

	rep.TotalEvents++
	rep.LastTs = time.Now().Unix()

	// Store updated reputation
	repKey := fmt.Sprintf("REPUTATION:%s:%s", rating.ActorID, rating.Dimension)
	repJSON, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal reputation: %v", err)
	}

	err = ctx.GetStub().PutState(repKey, repJSON)
	if err != nil {
		return fmt.Errorf("failed to store reputation: %v", err)
	}

	// Emit event
	score := rep.Alpha / (rep.Alpha + rep.Beta)
	eventPayload := map[string]interface{}{
		"actorId":     rating.ActorID,
		"dimension":   rating.Dimension,
		"newScore":    score,
		"totalEvents": rep.TotalEvents,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("ReputationUpdated", eventJSON)

	return nil
}

// calculateRaterWeight computes the rater's influence based on METAREPUTATION
func (rc *ReputationContract) calculateRaterWeight(
	ctx contractapi.TransactionContextInterface,
	raterID string,
	baseDimension string,
) (float64, error) {
	config, err := getConfig(ctx)
	if err != nil {
		return config.MinRaterWeight, err
	}

	// Get the META-dimension
	metaDimension, exists := config.MetaDimensions[baseDimension]
	if !exists {
		return config.MinRaterWeight, fmt.Errorf("no meta-dimension for %s", baseDimension)
	}

	// Load rater's METAREPUTATION
	rep, err := getOrInitReputation(ctx, raterID, metaDimension, config)
	if err != nil {
		return config.MinRaterWeight, err
	}

	// Apply dynamic time decay
	effectiveRep := applyDynamicDecay(rep, config)

	// Calculate metareputation score
	metaScore := effectiveRep.Alpha / (effectiveRep.Alpha + effectiveRep.Beta)

	// Calculate confidence factor
	totalEvents := effectiveRep.Alpha + effectiveRep.Beta
	confidenceFactor := 1.0 + math.Sqrt(totalEvents/(totalEvents+10.0))

	// Calculate weight
	weight := metaScore * confidenceFactor

	// Apply bounds
	if weight < config.MinRaterWeight {
		weight = config.MinRaterWeight
	}
	if weight > config.MaxRaterWeight {
		weight = config.MaxRaterWeight
	}

	return weight, nil
}

// ============================================================================
// DISPUTE RESOLUTION
// ============================================================================

// InitiateDispute allows challenging a rating
func (rc *ReputationContract) InitiateDispute(
	ctx contractapi.TransactionContextInterface,
	ratingID string,
	reason string,
) (string, error) {
	initiatorID, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return "", fmt.Errorf("failed to get initiator ID: %v", err)
	}

	normalizedInitiatorID := normalizeIdentity(initiatorID)

	// Load rating
	ratingJSON, err := ctx.GetStub().GetState(ratingID)
	if err != nil || ratingJSON == nil {
		return "", fmt.Errorf("rating not found: %s", ratingID)
	}

	var rating Rating
	if err := json.Unmarshal(ratingJSON, &rating); err != nil {
		return "", fmt.Errorf("failed to unmarshal rating: %v", err)
	}

	// Check initiator is the rated actor
	if normalizedInitiatorID != rating.ActorID {
		return "", fmt.Errorf("only the rated actor can dispute a rating")
	}

	// Check initiator has stake for dispute cost
	config, err := getConfig(ctx)
	if err != nil {
		return "", err
	}

	stake, err := getOrInitStake(ctx, normalizedInitiatorID)
	if err != nil {
		return "", err
	}

	if stake.Balance < config.DisputeCost {
		return "", fmt.Errorf("insufficient stake for dispute: %f required", config.DisputeCost)
	}

	// Lock dispute cost
	stake.Balance -= config.DisputeCost
	stake.Locked += config.DisputeCost
	stake.UpdatedAt = time.Now().Unix()

	stakeKey := fmt.Sprintf("STAKE:%s", normalizedInitiatorID)
	stakeJSON, _ := json.Marshal(stake)
	ctx.GetStub().PutState(stakeKey, stakeJSON)

	// Create dispute
	disputeID := generateDisputeID(ratingID, normalizedInitiatorID, time.Now().Unix())
	dispute := Dispute{
		DisputeID:   disputeID,
		RatingID:    ratingID,
		InitiatorID: normalizedInitiatorID,
		RaterID:     rating.RaterID,
		ActorID:     rating.ActorID,
		Dimension:   rating.Dimension,
		Reason:      reason,
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
	}

	disputeJSON, err := json.Marshal(dispute)
	if err != nil {
		return "", fmt.Errorf("failed to marshal dispute: %v", err)
	}

	err = ctx.GetStub().PutState(disputeID, disputeJSON)
	if err != nil {
		return "", fmt.Errorf("failed to store dispute: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"disputeId":   disputeID,
		"ratingId":    ratingID,
		"initiatorId": normalizedInitiatorID,
		"reason":      reason,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("DisputeInitiated", eventJSON)

	return disputeID, nil
}

// ResolveDispute allows arbitrator to resolve a dispute
func (rc *ReputationContract) ResolveDispute(
	ctx contractapi.TransactionContextInterface,
	disputeID string,
	verdict string,
	arbitratorNotes string,
) error {
	// Validate verdict
	if verdict != "upheld" && verdict != "overturned" {
		return fmt.Errorf("verdict must be 'upheld' or 'overturned'")
	}

	// Check arbitrator role
	if !isArbitrator(ctx) {
		return fmt.Errorf("unauthorized: arbitrator role required")
	}

	// Load dispute
	disputeJSON, err := ctx.GetStub().GetState(disputeID)
	if err != nil || disputeJSON == nil {
		return fmt.Errorf("dispute not found: %s", disputeID)
	}

	var dispute Dispute
	if err := json.Unmarshal(disputeJSON, &dispute); err != nil {
		return fmt.Errorf("failed to unmarshal dispute: %v", err)
	}

	if dispute.Status != "pending" {
		return fmt.Errorf("dispute already resolved")
	}

	// Get arbitrator ID
	arbitratorID, _ := ctx.GetClientIdentity().GetID()
	normalizedArbitratorID := normalizeIdentity(arbitratorID)

	// Update dispute record
	dispute.Status = verdict
	dispute.ArbitratorID = normalizedArbitratorID
	dispute.ArbitratorNotes = arbitratorNotes
	dispute.ResolvedAt = time.Now().Unix()

	// Determine if rater was correct
	raterWasCorrect := (verdict == "upheld")

	// Update METAREPUTATION
	err = rc.updateMetaReputation(ctx, dispute.RaterID, dispute.Dimension, raterWasCorrect)
	if err != nil {
		return fmt.Errorf("failed to update metareputation: %v", err)
	}

	// If overturned, reverse the rating's effect
	if verdict == "overturned" {
		err = rc.reverseRating(ctx, dispute.RatingID)
		if err != nil {
			return fmt.Errorf("failed to reverse rating: %v", err)
		}

		// Slash rater's stake
		err = rc.slashStake(ctx, dispute.RaterID)
		if err != nil {
			return fmt.Errorf("failed to slash stake: %v", err)
		}
	}

	// Return dispute cost to initiator
	config, _ := getConfig(ctx)
	stake, _ := getOrInitStake(ctx, dispute.InitiatorID)
	stake.Locked -= config.DisputeCost
	stake.Balance += config.DisputeCost
	stake.UpdatedAt = time.Now().Unix()

	stakeKey := fmt.Sprintf("STAKE:%s", dispute.InitiatorID)
	stakeJSON, _ := json.Marshal(stake)
	ctx.GetStub().PutState(stakeKey, stakeJSON)

	// Store updated dispute
	updatedDisputeJSON, _ := json.Marshal(dispute)
	ctx.GetStub().PutState(disputeID, updatedDisputeJSON)

	// Emit event
	eventPayload := map[string]interface{}{
		"disputeId":       disputeID,
		"verdict":         verdict,
		"raterWasCorrect": raterWasCorrect,
		"dimension":       dispute.Dimension,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("DisputeResolved", eventJSON)

	return nil
}

// updateMetaReputation updates rater's ability to rate others
func (rc *ReputationContract) updateMetaReputation(
	ctx contractapi.TransactionContextInterface,
	raterID string,
	baseDimension string,
	wasCorrect bool,
) error {
	config, err := getConfig(ctx)
	if err != nil {
		return err
	}

	// Get meta-dimension
	metaDimension, exists := config.MetaDimensions[baseDimension]
	if !exists {
		return fmt.Errorf("no meta-dimension for %s", baseDimension)
	}

	// Load or initialize metareputation
	rep, err := getOrInitReputation(ctx, raterID, metaDimension, config)
	if err != nil {
		return err
	}

	// Update based on dispute outcome
	if wasCorrect {
		rep.Alpha += 1.0 // Rater was right
	} else {
		rep.Beta += 1.0 // Rater was wrong
	}

	rep.LastTs = time.Now().Unix()
	rep.TotalEvents++

	// Store updated metareputation
	repKey := fmt.Sprintf("REPUTATION:%s:%s", raterID, metaDimension)
	repJSON, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal metareputation: %v", err)
	}

	return ctx.GetStub().PutState(repKey, repJSON)
}

// reverseRating undoes the effect of an overturned rating
func (rc *ReputationContract) reverseRating(
	ctx contractapi.TransactionContextInterface,
	ratingID string,
) error {
	// Load rating
	ratingJSON, err := ctx.GetStub().GetState(ratingID)
	if err != nil || ratingJSON == nil {
		return fmt.Errorf("rating not found: %s", ratingID)
	}

	var rating Rating
	if err := json.Unmarshal(ratingJSON, &rating); err != nil {
		return fmt.Errorf("failed to unmarshal rating: %v", err)
	}

	config, _ := getConfig(ctx)

	// Load actor's reputation
	rep, err := getOrInitReputation(ctx, rating.ActorID, rating.Dimension, config)
	if err != nil {
		return err
	}

	// Reverse the effect
	if rating.Value >= 0.5 {
		rep.Alpha -= rating.Weight * rating.Value
	} else {
		rep.Beta -= rating.Weight * (1.0 - rating.Value)
	}

	// Ensure non-negative
	if rep.Alpha < config.InitialAlpha {
		rep.Alpha = config.InitialAlpha
	}
	if rep.Beta < config.InitialBeta {
		rep.Beta = config.InitialBeta
	}

	rep.TotalEvents--

	// Store updated reputation
	repKey := fmt.Sprintf("REPUTATION:%s:%s", rating.ActorID, rating.Dimension)
	repJSON, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal reputation: %v", err)
	}

	return ctx.GetStub().PutState(repKey, repJSON)
}

// slashStake penalizes rater for false rating
func (rc *ReputationContract) slashStake(
	ctx contractapi.TransactionContextInterface,
	raterID string,
) error {
	config, err := getConfig(ctx)
	if err != nil {
		return err
	}

	stake, err := getOrInitStake(ctx, raterID)
	if err != nil {
		return err
	}

	slashAmount := stake.Balance * config.SlashPercentage
	stake.Balance -= slashAmount
	stake.UpdatedAt = time.Now().Unix()

	stakeKey := fmt.Sprintf("STAKE:%s", raterID)
	stakeJSON, err := json.Marshal(stake)
	if err != nil {
		return fmt.Errorf("failed to marshal stake: %v", err)
	}

	err = ctx.GetStub().PutState(stakeKey, stakeJSON)
	if err != nil {
		return fmt.Errorf("failed to store stake: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"raterId":     raterID,
		"slashAmount": slashAmount,
		"newBalance":  stake.Balance,
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("StakeSlashed", eventJSON)

	return nil
}

// ============================================================================
// QUERY FUNCTIONS
// ============================================================================

// GetReputation retrieves an actor's reputation with dynamic decay applied
func (rc *ReputationContract) GetReputation(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
) (map[string]interface{}, error) {
	config, err := getConfig(ctx)
	if err != nil {
		return nil, err
	}

	if !config.ValidDimensions[dimension] {
		return nil, fmt.Errorf("invalid dimension: %s", dimension)
	}

	normalizedActorID := normalizeIdentity(actorID)

	// Load reputation
	rep, err := getOrInitReputation(ctx, normalizedActorID, dimension, config)
	if err != nil {
		return nil, err
	}

	// Apply dynamic decay
	effectiveRep := applyDynamicDecay(rep, config)

	// Calculate score
	score := effectiveRep.Alpha / (effectiveRep.Alpha + effectiveRep.Beta)

	// Calculate Wilson confidence interval
	ci := calculateWilsonCI(effectiveRep.Alpha, effectiveRep.Beta, 0.95)

	result := map[string]interface{}{
		"actorId":     normalizedActorID,
		"dimension":   dimension,
		"score":       score,
		"alpha":       effectiveRep.Alpha,
		"beta":        effectiveRep.Beta,
		"ci_lower":    ci[0],
		"ci_upper":    ci[1],
		"totalEvents": rep.TotalEvents,
		"lastUpdated": rep.LastTs,
	}

	return result, nil
}

// GetRatingHistory retrieves all ratings for an actor
func (rc *ReputationContract) GetRatingHistory(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
) ([]Rating, error) {
	normalizedActorID := normalizeIdentity(actorID)

	// Construct CouchDB query
	query := fmt.Sprintf(`{
		"selector": {
			"actorId": "%s",
			"dimension": "%s"
		},
		"sort": [{"timestamp": "desc"}],
		"limit": 100
	}`, normalizedActorID, dimension)

	resultsIterator, err := ctx.GetStub().GetQueryResult(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer resultsIterator.Close()

	var ratings []Rating
	for resultsIterator.HasNext() {
		queryResponse, err := resultsIterator.Next()
		if err != nil {
			return nil, err
		}

		var rating Rating
		if err := json.Unmarshal(queryResponse.Value, &rating); err != nil {
			continue
		}
		ratings = append(ratings, rating)
	}

	return ratings, nil
}

// GetDisputesByStatus retrieves disputes by status
func (rc *ReputationContract) GetDisputesByStatus(
	ctx contractapi.TransactionContextInterface,
	status string,
) ([]Dispute, error) {
	query := fmt.Sprintf(`{
		"selector": {
			"status": "%s"
		},
		"sort": [{"createdAt": "desc"}],
		"limit": 100
	}`, status)

	resultsIterator, err := ctx.GetStub().GetQueryResult(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer resultsIterator.Close()

	var disputes []Dispute
	for resultsIterator.HasNext() {
		queryResponse, err := resultsIterator.Next()
		if err != nil {
			return nil, err
		}

		var dispute Dispute
		if err := json.Unmarshal(queryResponse.Value, &dispute); err != nil {
			continue
		}
		disputes = append(disputes, dispute)
	}

	return disputes, nil
}

// GetActorsByDimension retrieves actors with reputation above threshold
func (rc *ReputationContract) GetActorsByDimension(
	ctx contractapi.TransactionContextInterface,
	dimension string,
	minScoreStr string,
) ([]map[string]interface{}, error) {
	minScore, err := strconv.ParseFloat(minScoreStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid minScore: %v", err)
	}

	query := fmt.Sprintf(`{
		"selector": {
			"dimension": "%s"
		},
		"limit": 1000
	}`, dimension)

	resultsIterator, err := ctx.GetStub().GetQueryResult(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer resultsIterator.Close()

	config, _ := getConfig(ctx)

	var results []map[string]interface{}
	for resultsIterator.HasNext() {
		queryResponse, err := resultsIterator.Next()
		if err != nil {
			continue
		}

		var rep Reputation
		if err := json.Unmarshal(queryResponse.Value, &rep); err != nil {
			continue
		}

		// Apply dynamic decay and calculate score
		effectiveRep := applyDynamicDecay(&rep, config)
		score := effectiveRep.Alpha / (effectiveRep.Alpha + effectiveRep.Beta)

		// Filter by minimum score
		if score >= minScore {
			results = append(results, map[string]interface{}{
				"actorId":   rep.ActorID,
				"dimension": rep.Dimension,
				"score":     score,
			})
		}
	}

	return results, nil
}

// GetRatingsByRater retrieves all ratings submitted by a rater
func (rc *ReputationContract) GetRatingsByRater(
	ctx contractapi.TransactionContextInterface,
	raterID string,
) ([]Rating, error) {
	normalizedRaterID := normalizeIdentity(raterID)

	query := fmt.Sprintf(`{
		"selector": {
			"raterId": "%s"
		},
		"sort": [{"timestamp": "desc"}],
		"limit": 100
	}`, normalizedRaterID)

	resultsIterator, err := ctx.GetStub().GetQueryResult(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer resultsIterator.Close()

	var ratings []Rating
	for resultsIterator.HasNext() {
		queryResponse, err := resultsIterator.Next()
		if err != nil {
			return nil, err
		}

		var rating Rating
		if err := json.Unmarshal(queryResponse.Value, &rating); err != nil {
			continue
		}
		ratings = append(ratings, rating)
	}

	return ratings, nil
}

// GetDispute retrieves a specific dispute
func (rc *ReputationContract) GetDispute(
	ctx contractapi.TransactionContextInterface,
	disputeID string,
) (*Dispute, error) {
	disputeJSON, err := ctx.GetStub().GetState(disputeID)
	if err != nil {
		return nil, fmt.Errorf("failed to read dispute: %v", err)
	}
	if disputeJSON == nil {
		return nil, fmt.Errorf("dispute not found: %s", disputeID)
	}

	var dispute Dispute
	if err := json.Unmarshal(disputeJSON, &dispute); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dispute: %v", err)
	}

	return &dispute, nil
}

// GetRating retrieves a specific rating
func (rc *ReputationContract) GetRating(
	ctx contractapi.TransactionContextInterface,
	ratingID string,
) (*Rating, error) {
	ratingJSON, err := ctx.GetStub().GetState(ratingID)
	if err != nil {
		return nil, fmt.Errorf("failed to read rating: %v", err)
	}
	if ratingJSON == nil {
		return nil, fmt.Errorf("rating not found: %s", ratingID)
	}

	var rating Rating
	if err := json.Unmarshal(ratingJSON, &rating); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rating: %v", err)
	}

	return &rating, nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// *** FIX 5: ADD normalizeIdentity helper function ***
// normalizeIdentity extracts CN from X.509 DN or returns simplified ID
// normalizeIdentity extracts CN from identity string and makes it case-insensitive
func normalizeIdentity(identity string) string {
	// First, try to decode if it's base64
	decoded, err := base64.StdEncoding.DecodeString(identity)
	if err == nil {
		// Successfully decoded, use the decoded string
		identity = string(decoded)
	}
	
	// Handle X.509 DN format: "x509::CN=user1,OU=client::CN=ca.org1.example.com"
	if strings.Contains(identity, "x509::") {
		parts := strings.Split(identity, "::")
		if len(parts) >= 2 {
			cnPart := parts[1]
			cnFields := strings.Split(cnPart, ",")
			for _, field := range cnFields {
				trimmed := strings.TrimSpace(field)
				if strings.HasPrefix(strings.ToUpper(trimmed), "CN=") {
					cn := strings.TrimPrefix(trimmed, "CN=")
					cn = strings.TrimPrefix(cn, "cn=")
					// Return lowercase for case-insensitive comparison
					return strings.ToLower(cn)
				}
			}
		}
	}
	
	// If no x509 format, just return lowercase
	return strings.ToLower(identity)
}
// getConfig retrieves system configuration
// getConfig retrieves system configuration, initializing if needed
func getConfig(ctx contractapi.TransactionContextInterface) (*SystemConfig, error) {
	configJSON, err := ctx.GetStub().GetState("SYSTEM_CONFIG")
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %v", err)
	}

	// AUTO-INITIALIZE if config doesn't exist
	if configJSON == nil {
		config := SystemConfig{
			MinStakeRequired: 10000.0,
			DisputeCost:      100.0,
			SlashPercentage:  0.1,
			DecayRate:        0.98,
			DecayPeriod:      86400.0,
			InitialAlpha:     2.0,
			InitialBeta:      2.0,
			MinRaterWeight:   0.1,
			MaxRaterWeight:   5.0,
			ValidDimensions: map[string]bool{
				"quality":    true,
				"delivery":   true,
				"compliance": true,
				"warranty":   true,
			},
			MetaDimensions: map[string]string{
				"quality":    "rating_quality",
				"delivery":   "rating_delivery",
				"compliance": "rating_compliance",
				"warranty":   "rating_warranty",
			},
			Version:     1,
			LastUpdated: time.Now().Unix(),
		}

		configJSON, err = json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %v", err)
		}

		err = ctx.GetStub().PutState("SYSTEM_CONFIG", configJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to auto-initialize config: %v", err)
		}

		return &config, nil
	}

	var config SystemConfig
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return &config, nil
}
// validateConfig validates system configuration
func validateConfig(config *SystemConfig) error {
	if config.MinStakeRequired < 0 {
		return fmt.Errorf("minStakeRequired must be non-negative")
	}
	if config.DisputeCost < 0 {
		return fmt.Errorf("disputeCost must be non-negative")
	}
	if config.SlashPercentage < 0 || config.SlashPercentage > 1 {
		return fmt.Errorf("slashPercentage must be between 0 and 1")
	}
	if config.DecayRate < 0 || config.DecayRate > 1 {
		return fmt.Errorf("decayRate must be between 0 and 1")
	}
	if config.DecayPeriod <= 0 {
		return fmt.Errorf("decayPeriod must be positive")
	}
	if config.InitialAlpha <= 0 || config.InitialBeta <= 0 {
		return fmt.Errorf("initial alpha and beta must be positive")
	}
	if config.MinRaterWeight < 0 || config.MaxRaterWeight < config.MinRaterWeight {
		return fmt.Errorf("invalid rater weight bounds")
	}
	if len(config.ValidDimensions) == 0 {
		return fmt.Errorf("at least one valid dimension required")
	}

	return nil
}

// getOrInitReputation loads or initializes reputation
func getOrInitReputation(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
	config *SystemConfig,
) (*Reputation, error) {
	repKey := fmt.Sprintf("REPUTATION:%s:%s", actorID, dimension)
	repJSON, err := ctx.GetStub().GetState(repKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read reputation: %v", err)
	}

	if repJSON == nil {
		// Initialize new reputation
		return &Reputation{
			ActorID:     actorID,
			Dimension:   dimension,
			Alpha:       config.InitialAlpha,
			Beta:        config.InitialBeta,
			TotalEvents: 0,
			LastTs:      time.Now().Unix(),
		}, nil
	}

	var rep Reputation
	if err := json.Unmarshal(repJSON, &rep); err != nil {
		return nil, fmt.Errorf("failed to unmarshal reputation: %v", err)
	}

	return &rep, nil
}

// getOrInitStake loads or initializes stake
func getOrInitStake(
	ctx contractapi.TransactionContextInterface,
	actorID string,
) (*Stake, error) {
	stakeKey := fmt.Sprintf("STAKE:%s", actorID)
	stakeJSON, err := ctx.GetStub().GetState(stakeKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read stake: %v", err)
	}

	if stakeJSON == nil {
		// Initialize new stake
		return &Stake{
			ActorID:   actorID,
			Balance:   0.0,
			Locked:    0.0,
			UpdatedAt: time.Now().Unix(),
		}, nil
	}

	var stake Stake
	if err := json.Unmarshal(stakeJSON, &stake); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stake: %v", err)
	}

	return &stake, nil
}

// applyDynamicDecay applies variance-based time decay to reputation
func applyDynamicDecay(rep *Reputation, config *SystemConfig) *Reputation {
	now := time.Now().Unix()
	timeDelta := float64(now - rep.LastTs)

	// Calculate Beta distribution variance
	alpha := rep.Alpha
	beta := rep.Beta
	sum := alpha + beta
	variance := (alpha * beta) / (sum * sum * (sum + 1))

	// Normalize variance (max variance ≈ 0.083 at α=β=1)
	maxVariance := 0.083
	normalizedVariance := math.Min(variance/maxVariance, 1.0)

	// Adaptive decay: high variance → faster decay
	adaptiveDecayRate := config.DecayRate + (1.0-config.DecayRate)*normalizedVariance*0.5

	// Apply decay
	decayFactor := math.Pow(adaptiveDecayRate, timeDelta/config.DecayPeriod)

	effectiveAlpha := alpha * decayFactor
	effectiveBeta := beta * decayFactor

	// Prevent decay below initial values
	if effectiveAlpha < config.InitialAlpha {
		effectiveAlpha = config.InitialAlpha
	}
	if effectiveBeta < config.InitialBeta {
		effectiveBeta = config.InitialBeta
	}

	return &Reputation{
		ActorID:     rep.ActorID,
		Dimension:   rep.Dimension,
		Alpha:       effectiveAlpha,
		Beta:        effectiveBeta,
		TotalEvents: rep.TotalEvents,
		LastTs:      rep.LastTs,
	}
}

// calculateWilsonCI computes Wilson score confidence interval
func calculateWilsonCI(alpha, beta, confidence float64) [2]float64 {
	n := alpha + beta
	if n == 0 {
		return [2]float64{0, 0}
	}

	p := alpha / n
	z := 1.96 // 95% confidence
	if confidence == 0.99 {
		z = 2.576
	}

	denominator := 1 + (z * z / n)
	centre := (p + (z*z)/(2*n)) / denominator
	margin := (z * math.Sqrt((p*(1-p))/n+(z*z)/(4*n*n))) / denominator

	lower := centre - margin
	upper := centre + margin

	if lower < 0 {
		lower = 0
	}
	if upper > 1 {
		upper = 1
	}

	return [2]float64{lower, upper}
}

// generateRatingID creates unique rating identifier
func generateRatingID(raterID, actorID, dimension string, timestamp int64) string {
	data := fmt.Sprintf("%s:%s:%s:%d", raterID, actorID, dimension, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("RATING:%x", hash[:16])
}

// generateDisputeID creates unique dispute identifier
func generateDisputeID(ratingID, initiatorID string, timestamp int64) string {
	data := fmt.Sprintf("%s:%s:%d", ratingID, initiatorID, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("DISPUTE:%x", hash[:16])
}

// isAdmin checks if caller has admin privileges
func isAdmin(ctx contractapi.TransactionContextInterface) bool {
	// Option 1: Check MSP attribute
	val, ok, _ := ctx.GetClientIdentity().GetAttributeValue("admin")
	if ok && val == "true" {
		return true
	}

	// Option 2: Check against admin list
	adminListJSON, err := ctx.GetStub().GetState("ADMIN_LIST")
	if err == nil && adminListJSON != nil {
		var admins map[string]bool
		if err := json.Unmarshal(adminListJSON, &admins); err == nil {
			callerID, _ := ctx.GetClientIdentity().GetID()
			normalizedCallerID := normalizeIdentity(callerID)
			return admins[normalizedCallerID]
		}
	}

	return false
}

// isArbitrator checks if caller has arbitrator privileges
func isArbitrator(ctx contractapi.TransactionContextInterface) bool {
	// Check MSP attribute
	val, ok, _ := ctx.GetClientIdentity().GetAttributeValue("arbitrator")
	if ok && val == "true" {
		return true
	}

	// Check against arbitrator list
	arbitratorListJSON, err := ctx.GetStub().GetState("ARBITRATOR_LIST")
	if err == nil && arbitratorListJSON != nil {
		var arbitrators map[string]bool
		if err := json.Unmarshal(arbitratorListJSON, &arbitrators); err == nil {
			callerID, _ := ctx.GetClientIdentity().GetID()
			normalizedCallerID := normalizeIdentity(callerID)
			return arbitrators[normalizedCallerID]
		}
	}

	return false
}

// ============================================================================
// ADMIN MANAGEMENT FUNCTIONS
// ============================================================================

// AddAdmin adds a new administrator
func (rc *ReputationContract) AddAdmin(
	ctx contractapi.TransactionContextInterface,
	newAdminID string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: only admin can add admins")
	}

	normalizedAdminID := normalizeIdentity(newAdminID)

	// Load or initialize admin list
	adminListJSON, err := ctx.GetStub().GetState("ADMIN_LIST")
	admins := make(map[string]bool)
	if err == nil && adminListJSON != nil {
		json.Unmarshal(adminListJSON, &admins)
	}

	// Add new admin
	admins[normalizedAdminID] = true

	// Store updated list
	updatedJSON, _ := json.Marshal(admins)
	err = ctx.GetStub().PutState("ADMIN_LIST", updatedJSON)
	if err != nil {
		return fmt.Errorf("failed to update admin list: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"adminId": normalizedAdminID,
		"action":  "added",
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("AdminUpdated", eventJSON)

	return nil
}

// RemoveAdmin removes an administrator
func (rc *ReputationContract) RemoveAdmin(
	ctx contractapi.TransactionContextInterface,
	adminID string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: only admin can remove admins")
	}

	normalizedAdminID := normalizeIdentity(adminID)

	// Load admin list
	adminListJSON, err := ctx.GetStub().GetState("ADMIN_LIST")
	if err != nil || adminListJSON == nil {
		return fmt.Errorf("admin list not found")
	}

	var admins map[string]bool
	json.Unmarshal(adminListJSON, &admins)

	// Remove admin
	delete(admins, normalizedAdminID)

	// Ensure at least one admin remains
	if len(admins) == 0 {
		return fmt.Errorf("cannot remove last admin")
	}

	// Store updated list
	updatedJSON, _ := json.Marshal(admins)
	err = ctx.GetStub().PutState("ADMIN_LIST", updatedJSON)
	if err != nil {
		return fmt.Errorf("failed to update admin list: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"adminId": normalizedAdminID,
		"action":  "removed",
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("AdminUpdated", eventJSON)

	return nil
}

// AddArbitrator adds a new arbitrator
func (rc *ReputationContract) AddArbitrator(
	ctx contractapi.TransactionContextInterface,
	arbitratorID string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: only admin can add arbitrators")
	}

	normalizedArbitratorID := normalizeIdentity(arbitratorID)

	// Load or initialize arbitrator list
	arbitratorListJSON, err := ctx.GetStub().GetState("ARBITRATOR_LIST")
	arbitrators := make(map[string]bool)
	if err == nil && arbitratorListJSON != nil {
		json.Unmarshal(arbitratorListJSON, &arbitrators)
	}

	// Add new arbitrator
	arbitrators[normalizedArbitratorID] = true

	// Store updated list
	updatedJSON, _ := json.Marshal(arbitrators)
	err = ctx.GetStub().PutState("ARBITRATOR_LIST", updatedJSON)
	if err != nil {
		return fmt.Errorf("failed to update arbitrator list: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"arbitratorId": normalizedArbitratorID,
		"action":       "added",
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("ArbitratorUpdated", eventJSON)

	return nil
}

// RemoveArbitrator removes an arbitrator
func (rc *ReputationContract) RemoveArbitrator(
	ctx contractapi.TransactionContextInterface,
	arbitratorID string,
) error {
	if !isAdmin(ctx) {
		return fmt.Errorf("unauthorized: only admin can remove arbitrators")
	}

	normalizedArbitratorID := normalizeIdentity(arbitratorID)

	// Load arbitrator list
	arbitratorListJSON, err := ctx.GetStub().GetState("ARBITRATOR_LIST")
	if err != nil || arbitratorListJSON == nil {
		return fmt.Errorf("arbitrator list not found")
	}

	var arbitrators map[string]bool
	json.Unmarshal(arbitratorListJSON, &arbitrators)

	// Remove arbitrator
	delete(arbitrators, normalizedArbitratorID)

	// Store updated list
	updatedJSON, _ := json.Marshal(arbitrators)
	err = ctx.GetStub().PutState("ARBITRATOR_LIST", updatedJSON)
	if err != nil {
		return fmt.Errorf("failed to update arbitrator list: %v", err)
	}

	// Emit event
	eventPayload := map[string]interface{}{
		"arbitratorId": normalizedArbitratorID,
		"action":       "removed",
	}
	eventJSON, _ := json.Marshal(eventPayload)
	ctx.GetStub().SetEvent("ArbitratorUpdated", eventJSON)

	return nil
}
// ResetStake - TEST ONLY: Reset an actor's stake to zero
// In production, remove this function or add proper access controls
func (rc *ReputationContract) ResetStake(
	ctx contractapi.TransactionContextInterface,
	actorID string,
) error {
	normalizedID := normalizeIdentity(actorID)
	
	stake := &Stake{
		ActorID:   normalizedID,
		Balance:   0,
		Locked:    0,
		UpdatedAt: time.Now().Unix(),
	}

	stakeKey := fmt.Sprintf("STAKE:%s", normalizedID)
	stakeJSON, err := json.Marshal(stake)
	if err != nil {
		return fmt.Errorf("failed to marshal stake: %v", err)
	}

	return ctx.GetStub().PutState(stakeKey, stakeJSON)
}
// ============================================================================
// MAIN FUNCTION
// ============================================================================

func main() {
	chaincode, err := contractapi.NewChaincode(&ReputationContract{})
	if err != nil {
		fmt.Printf("Error creating reputation chaincode: %v\n", err)
		return
	}

	if err := chaincode.Start(); err != nil {
		fmt.Printf("Error starting reputation chaincode: %v", err)
	}
}
