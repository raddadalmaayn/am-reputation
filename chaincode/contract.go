package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// Reputation stores Beta distribution parameters for each (actor, dimension)
type Reputation struct {
	ActorID   string  `json:"actorId"`
	Dimension string  `json:"dimension"`
	Alpha     float64 `json:"alpha"`           // Success count
	Beta      float64 `json:"beta"`            // Failure count
	LastTs    int64   `json:"lastTs"`          // Last update timestamp
	TotalEvents int64 `json:"totalEvents"`     // Total ratings received
}

// Rating represents a single rating event
type Rating struct {
	RatingID  string  `json:"ratingId"`        // Unique ID
	RaterID   string  `json:"raterId"`         // Who is rating
	ActorID   string  `json:"actorId"`         // Who is being rated
	Dimension string  `json:"dimension"`       // quality, delivery, compliance, etc.
	Value     float64 `json:"value"`           // 0.0-1.0 (0=failure, 1=success)
	Weight    float64 `json:"weight"`          // Rater's weight (based on their reputation)
	Evidence  string  `json:"evidence"`        // CID or hash of off-chain evidence
	Timestamp int64   `json:"timestamp"`
	TxID      string  `json:"txId"`            // Transaction ID for auditability
}

// Stake represents locked capital for a rater
type Stake struct {
	RaterID       string  `json:"raterId"`
	Amount        float64 `json:"amount"`
	LockedAmount  float64 `json:"lockedAmount"`  // Amount locked in pending ratings
	LastUpdated   int64   `json:"lastUpdated"`
}

// Dispute represents a challenged rating
type Dispute struct {
	DisputeID     string `json:"disputeId"`
	RatingID      string `json:"ratingId"`
	Challenger    string `json:"challenger"`
	Reason        string `json:"reason"`
	Evidence      string `json:"evidence"`
	Status        string `json:"status"`        // pending, resolved
	Verdict       string `json:"verdict"`       // upheld, overturned
	ArbitratorID  string `json:"arbitratorId"`
	Timestamp     int64  `json:"timestamp"`
	ResolvedTs    int64  `json:"resolvedTs"`
}

// SystemMetrics tracks overall system statistics for KPI analysis
type SystemMetrics struct {
	TotalRatings       int64   `json:"totalRatings"`
	TotalDisputes      int64   `json:"totalDisputes"`
	DisputesUpheld     int64   `json:"disputesUpheld"`
	DisputesOverturned int64   `json:"disputesOverturned"`
	TotalStakeSlashed  float64 `json:"totalStakeSlashed"`
	TotalActors        int64   `json:"totalActors"`
	LastUpdated        int64   `json:"lastUpdated"`
}

// AttackEvent logs detected suspicious activity for analysis
type AttackEvent struct {
	EventID     string  `json:"eventId"`
	EventType   string  `json:"eventType"`    // sybil, collusion, bribery
	ActorIDs    []string `json:"actorIds"`    // Involved actors
	Confidence  float64 `json:"confidence"`   // Detection confidence 0-1
	Description string  `json:"description"`
	Timestamp   int64   `json:"timestamp"`
	Detected    bool    `json:"detected"`
}

// PerformanceLog tracks transaction performance for benchmarking
type PerformanceLog struct {
	TxID         string `json:"txId"`
	Operation    string `json:"operation"`
	Latency      int64  `json:"latency"`      // Microseconds
	PayloadSize  int    `json:"payloadSize"`  // Bytes
	Timestamp    int64  `json:"timestamp"`
}

// ============================================================================
// CONTRACT
// ============================================================================

type ReputationContract struct {
	contractapi.Contract
}

// ============================================================================
// CONFIGURATION PARAMETERS (Tunable via MARL optimization)
// ============================================================================

const (
	// Reputation parameters
	DecayRate           = 0.98   // λ - time decay factor
	DecayPeriod         = 86400  // Decay period in seconds (1 day)
	MinStakeRequired    = 1000.0 // Minimum stake to submit ratings
	MaxRaterWeight      = 5.0    // Maximum weight a rater can have
	MinRaterWeight      = 0.1    // Minimum weight for new/low-rep raters
	
	// Attack detection thresholds
	SybilThreshold      = 0.15   // Variance threshold for sybil detection
	CollusionThreshold  = 0.20   // Correlation threshold for collusion
	
	// Dispute parameters
	DisputeCost         = 100.0  // Cost to initiate dispute
	SlashPercentage     = 0.30   // % of stake slashed if rating overturned
)

// ============================================================================
// COMPOSITE KEYS
// ============================================================================

const (
	prefixReputation    = "REP"
	prefixRating        = "RAT"
	prefixStake         = "STK"
	prefixDispute       = "DIS"
	prefixMetrics       = "MET"
	prefixAttackEvent   = "ATK"
	prefixPerformance   = "PRF"
)

func reputationKey(ctx contractapi.TransactionContextInterface, actorID, dimension string) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixReputation, []string{actorID, dimension})
}

func ratingKey(ctx contractapi.TransactionContextInterface, ratingID string) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixRating, []string{ratingID})
}

func stakeKey(ctx contractapi.TransactionContextInterface, raterID string) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixStake, []string{raterID})
}

func disputeKey(ctx contractapi.TransactionContextInterface, disputeID string) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixDispute, []string{disputeID})
}

func metricsKey(ctx contractapi.TransactionContextInterface) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixMetrics, []string{"global"})
}

func attackEventKey(ctx contractapi.TransactionContextInterface, eventID string) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixAttackEvent, []string{eventID})
}

func performanceKey(ctx contractapi.TransactionContextInterface, txID string) (string, error) {
	return ctx.GetStub().CreateCompositeKey(prefixPerformance, []string{txID})
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func trim(s string) string { return strings.TrimSpace(s) }

func getOrInitReputation(ctx contractapi.TransactionContextInterface, actorID, dimension string) (*Reputation, error) {
	key, err := reputationKey(ctx, actorID, dimension)
	if err != nil {
		return nil, err
	}
	
	raw, err := ctx.GetStub().GetState(key)
	if err != nil {
		return nil, err
	}
	
	if len(raw) == 0 {
		// Initialize with uniform prior (1,1)
		return &Reputation{
			ActorID:   actorID,
			Dimension: dimension,
			Alpha:     1.0,
			Beta:      1.0,
			LastTs:    time.Now().Unix(),
			TotalEvents: 0,
		}, nil
	}
	
	var rep Reputation
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func putState(ctx contractapi.TransactionContextInterface, key string, value interface{}) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ctx.GetStub().PutState(key, bytes)
}

func getState(ctx contractapi.TransactionContextInterface, key string, result interface{}) error {
	bytes, err := ctx.GetStub().GetState(key)
	if err != nil {
		return err
	}
	if len(bytes) == 0 {
		return fmt.Errorf("not found: %s", key)
	}
	return json.Unmarshal(bytes, result)
}

// ============================================================================
// CORE REPUTATION LOGIC
// ============================================================================

// SubmitRating - Main transaction for submitting ratings
// KPI tracked: Latency, Throughput, Storage overhead
func (rc *ReputationContract) SubmitRating(
	ctx contractapi.TransactionContextInterface,
	actorID string,      // Who is being rated
	dimension string,    // quality, delivery, compliance, warranty
	valueStr string,     // 0.0-1.0 (success probability)
	evidence string,     // CID/hash of off-chain evidence
) (string, error) {
	startTime := time.Now()
	
	// Input validation
	actorID = trim(actorID)
	dimension = trim(dimension)
	evidence = trim(evidence)
	
	if actorID == "" || dimension == "" {
		return "", errors.New("actorID and dimension required")
	}
	
	value, err := strconv.ParseFloat(trim(valueStr), 64)
	if err != nil || value < 0 || value > 1 {
		return "", errors.New("value must be between 0 and 1")
	}
	
	// Get rater identity
	raterID, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return "", fmt.Errorf("failed to get rater identity: %w", err)
	}
	
	// Check if rater has sufficient stake
	stakeObj, err := rc.getOrInitStake(ctx, raterID)
	if err != nil {
		return "", err
	}
	
	if stakeObj.Amount - stakeObj.LockedAmount < MinStakeRequired {
		return "", fmt.Errorf("insufficient stake: need %.2f, have %.2f available", 
			MinStakeRequired, stakeObj.Amount - stakeObj.LockedAmount)
	}
	
	// Calculate rater's weight based on their reputation
	raterWeight, err := rc.calculateRaterWeight(ctx, raterID, dimension)
	if err != nil {
		return "", err
	}
	
	// Generate unique rating ID
	txID := ctx.GetStub().GetTxID()
	timestamp := time.Now().Unix()
	ratingID := generateRatingID(raterID, actorID, dimension, timestamp)
	
	// Check for duplicate (idempotency)
	rKey, _ := ratingKey(ctx, ratingID)
	existing, _ := ctx.GetStub().GetState(rKey)
	if len(existing) > 0 {
		return ratingID, nil // Already processed
	}
	
	// Load current reputation with time decay
	rep, err := getOrInitReputation(ctx, actorID, dimension)
	if err != nil {
		return "", err
	}
	
	// Apply time decay
	timeSinceLastUpdate := float64(timestamp - rep.LastTs)
	decayFactor := math.Pow(DecayRate, timeSinceLastUpdate/DecayPeriod)
	rep.Alpha *= decayFactor
	rep.Beta *= decayFactor
	
	// Update with new rating (weighted)
	if value >= 0.5 {
		rep.Alpha += raterWeight * value
	} else {
		rep.Beta += raterWeight * (1.0 - value)
	}
	rep.LastTs = timestamp
	rep.TotalEvents++
	
	// Store rating record
	rating := Rating{
		RatingID:  ratingID,
		RaterID:   raterID,
		ActorID:   actorID,
		Dimension: dimension,
		Value:     value,
		Weight:    raterWeight,
		Evidence:  evidence,
		Timestamp: timestamp,
		TxID:      txID,
	}
	
	rKey, _ = ratingKey(ctx, ratingID)
	if err := putState(ctx, rKey, &rating); err != nil {
		return "", err
	}
	
	// Store updated reputation
	repKey, _ := reputationKey(ctx, actorID, dimension)
	if err := putState(ctx, repKey, rep); err != nil {
		return "", err
	}
	
	// Update system metrics
	if err := rc.incrementMetrics(ctx, "rating"); err != nil {
		// Log but don't fail transaction
		fmt.Printf("Warning: failed to update metrics: %v\n", err)
	}
	
	// Log performance for KPI analysis
	latency := time.Since(startTime).Microseconds()
	payloadSize := len(actorID) + len(dimension) + len(evidence) + 100
	rc.logPerformance(ctx, txID, "SubmitRating", latency, payloadSize)
	
	// Detect potential attacks
	rc.detectAnomalies(ctx, actorID, raterID, dimension, value)
	
	return ratingID, nil
}

// GetReputation - Query reputation score with confidence interval
// KPI tracked: Query latency
func (rc *ReputationContract) GetReputation(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
) (map[string]interface{}, error) {
	startTime := time.Now()
	
	rep, err := getOrInitReputation(ctx, actorID, dimension)
	if err != nil {
		return nil, err
	}
	
	// Apply time decay before returning score
	now := time.Now().Unix()
	timeSinceLastUpdate := float64(now - rep.LastTs)
	decayFactor := math.Pow(DecayRate, timeSinceLastUpdate/DecayPeriod)
	
	effectiveAlpha := rep.Alpha * decayFactor
	effectiveBeta := rep.Beta * decayFactor
	
	// Point estimate (mean of Beta distribution)
	score := effectiveAlpha / (effectiveAlpha + effectiveBeta)
	
	// Confidence interval (Wilson score)
	ci := calculateWilsonCI(effectiveAlpha, effectiveBeta, 0.95)
	
	// Confidence level (based on sample size)
	confidence := 1.0 - (1.0 / (1.0 + rep.TotalEvents))
	
	result := map[string]interface{}{
		"actorId":      actorID,
		"dimension":    dimension,
		"score":        score,
		"confidence":   confidence,
		"ci_lower":     ci[0],
		"ci_upper":     ci[1],
		"alpha":        effectiveAlpha,
		"beta":         effectiveBeta,
		"totalEvents":  rep.TotalEvents,
		"lastUpdated":  rep.LastTs,
		"queryLatency": time.Since(startTime).Microseconds(),
	}
	
	return result, nil
}

// ============================================================================
// STAKING MECHANISM
// ============================================================================

func (rc *ReputationContract) getOrInitStake(ctx contractapi.TransactionContextInterface, raterID string) (*Stake, error) {
	key, _ := stakeKey(ctx, raterID)
	var stake Stake
	err := getState(ctx, key, &stake)
	if err != nil {
		// Initialize new stake
		return &Stake{
			RaterID:      raterID,
			Amount:       0,
			LockedAmount: 0,
			LastUpdated:  time.Now().Unix(),
		}, nil
	}
	return &stake, nil
}

// AddStake - Rater deposits stake to participate
func (rc *ReputationContract) AddStake(
	ctx contractapi.TransactionContextInterface,
	amountStr string,
) error {
	raterID, _ := ctx.GetClientIdentity().GetID()
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		return errors.New("invalid amount")
	}
	
	stake, err := rc.getOrInitStake(ctx, raterID)
	if err != nil {
		return err
	}
	
	stake.Amount += amount
	stake.LastUpdated = time.Now().Unix()
	
	key, _ := stakeKey(ctx, raterID)
	return putState(ctx, key, stake)
}

// SlashStake - Penalty for dishonest ratings (called by dispute resolution)
func (rc *ReputationContract) SlashStake(
	ctx contractapi.TransactionContextInterface,
	raterID string,
	amountStr string,
) error {
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		return errors.New("invalid amount")
	}
	
	stake, err := rc.getOrInitStake(ctx, raterID)
	if err != nil {
		return err
	}
	
	if stake.Amount < amount {
		amount = stake.Amount // Slash everything if insufficient
	}
	
	stake.Amount -= amount
	stake.LastUpdated = time.Now().Unix()
	
	key, _ := stakeKey(ctx, raterID)
	if err := putState(ctx, key, stake); err != nil {
		return err
	}
	
	// Update metrics
	return rc.incrementMetrics(ctx, "slash")
}

// ============================================================================
// DISPUTE MECHANISM
// ============================================================================

// InitiateDispute - Challenge a rating
func (rc *ReputationContract) InitiateDispute(
	ctx contractapi.TransactionContextInterface,
	ratingID string,
	reason string,
	evidence string,
) (string, error) {
	challengerID, _ := ctx.GetClientIdentity().GetID()
	
	// Check if rating exists
	rKey, _ := ratingKey(ctx, ratingID)
	var rating Rating
	if err := getState(ctx, rKey, &rating); err != nil {
		return "", fmt.Errorf("rating not found: %w", err)
	}
	
	// Check challenger has stake to cover dispute cost
	stake, _ := rc.getOrInitStake(ctx, challengerID)
	if stake.Amount < DisputeCost {
		return "", fmt.Errorf("insufficient stake for dispute: need %.2f", DisputeCost)
	}
	
	// Create dispute
	disputeID := generateDisputeID(ratingID, challengerID)
	dispute := Dispute{
		DisputeID:    disputeID,
		RatingID:     ratingID,
		Challenger:   challengerID,
		Reason:       reason,
		Evidence:     evidence,
		Status:       "pending",
		Verdict:      "",
		ArbitratorID: "",
		Timestamp:    time.Now().Unix(),
		ResolvedTs:   0,
	}
	
	dKey, _ := disputeKey(ctx, disputeID)
	if err := putState(ctx, dKey, &dispute); err != nil {
		return "", err
	}
	
	rc.incrementMetrics(ctx, "dispute")
	
	return disputeID, nil
}

// ResolveDispute - Arbitrator resolves dispute
func (rc *ReputationContract) ResolveDispute(
	ctx contractapi.TransactionContextInterface,
	disputeID string,
	verdict string, // "upheld" or "overturned"
) error {
	arbitratorID, _ := ctx.GetClientIdentity().GetID()
	
	// Load dispute
	dKey, _ := disputeKey(ctx, disputeID)
	var dispute Dispute
	if err := getState(ctx, dKey, &dispute); err != nil {
		return err
	}
	
	if dispute.Status != "pending" {
		return errors.New("dispute already resolved")
	}
	
	// Load original rating
	rKey, _ := ratingKey(ctx, dispute.RatingID)
	var rating Rating
	if err := getState(ctx, rKey, &rating); err != nil {
		return err
	}
	
	dispute.Verdict = verdict
	dispute.Status = "resolved"
	dispute.ArbitratorID = arbitratorID
	dispute.ResolvedTs = time.Now().Unix()
	
	if verdict == "overturned" {
		// Slash rater's stake
		slashAmount := MinStakeRequired * SlashPercentage
		if err := rc.SlashStake(ctx, rating.RaterID, fmt.Sprintf("%.2f", slashAmount)); err != nil {
			return err
		}
		
		// Revert reputation update
		if err := rc.revertRating(ctx, &rating); err != nil {
			return err
		}
		
		rc.incrementMetrics(ctx, "overturned")
	} else {
		rc.incrementMetrics(ctx, "upheld")
	}
	
	return putState(ctx, dKey, &dispute)
}

func (rc *ReputationContract) revertRating(ctx contractapi.TransactionContextInterface, rating *Rating) error {
	rep, err := getOrInitReputation(ctx, rating.ActorID, rating.Dimension)
	if err != nil {
		return err
	}
	
	// Reverse the rating effect
	if rating.Value >= 0.5 {
		rep.Alpha -= rating.Weight * rating.Value
	} else {
		rep.Beta -= rating.Weight * (1.0 - rating.Value)
	}
	
	if rep.Alpha < 1.0 { rep.Alpha = 1.0 }
	if rep.Beta < 1.0 { rep.Beta = 1.0 }
	
	key, _ := reputationKey(ctx, rating.ActorID, rating.Dimension)
	return putState(ctx, key, rep)
}

// ============================================================================
// RATER WEIGHT CALCULATION
// ============================================================================

func (rc *ReputationContract) calculateRaterWeight(
	ctx contractapi.TransactionContextInterface,
	raterID string,
	dimension string,
) (float64, error) {
	// Get rater's own reputation in this dimension
	rep, err := getOrInitReputation(ctx, raterID, dimension)
	if err != nil {
		return MinRaterWeight, nil // Default for new raters
	}
	
	// Apply time decay
	now := time.Now().Unix()
	timeSinceLastUpdate := float64(now - rep.LastTs)
	decayFactor := math.Pow(DecayRate, timeSinceLastUpdate/DecayPeriod)
	
	effectiveAlpha := rep.Alpha * decayFactor
	effectiveBeta := rep.Beta * decayFactor
	
	score := effectiveAlpha / (effectiveAlpha + effectiveBeta)
	
	// Weight = score * sqrt(confidence)
	// Confidence increases with number of observations
	confidence := math.Sqrt(float64(rep.TotalEvents) / (float64(rep.TotalEvents) + 10.0))
	weight := score * (1.0 + confidence)
	
	// Clamp to valid range
	if weight < MinRaterWeight {
		weight = MinRaterWeight
	}
	if weight > MaxRaterWeight {
		weight = MaxRaterWeight
	}
	
	return weight, nil
}

// ============================================================================
// ATTACK DETECTION (for KPI analysis)
// ============================================================================

func (rc *ReputationContract) detectAnomalies(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	raterID string,
	dimension string,
	value float64,
) {
	// Simple heuristics for demonstration
	// Real implementation would use more sophisticated statistical analysis
	
	// Check for rating pattern anomalies
	// This is a placeholder - actual detection would analyze:
	// - Rating variance over time
	// - Correlation between raters
	// - Timing patterns
	// - Graph centrality metrics
	
	// For now, just log suspicious patterns for analysis
	eventID := fmt.Sprintf("ATK-%d-%s-%s", time.Now().Unix(), actorID, raterID)
	
	// Example: Detect potential sybil if rater is new but giving extreme ratings
	raterRep, _ := getOrInitReputation(ctx, raterID, dimension)
	if raterRep.TotalEvents < 5 && (value < 0.1 || value > 0.9) {
		event := AttackEvent{
			EventID:     eventID,
			EventType:   "potential_sybil",
			ActorIDs:    []string{raterID, actorID},
			Confidence:  0.6,
			Description: "New rater giving extreme rating",
			Timestamp:   time.Now().Unix(),
			Detected:    true,
		}
		
		key, _ := attackEventKey(ctx, eventID)
		putState(ctx, key, &event)
	}
}

// ============================================================================
// METRICS AND ANALYTICS
// ============================================================================

func (rc *ReputationContract) incrementMetrics(ctx contractapi.TransactionContextInterface, metricType string) error {
	key, _ := metricsKey(ctx)
	var metrics SystemMetrics
	
	err := getState(ctx, key, &metrics)
	if err != nil {
		// Initialize if doesn't exist
		metrics = SystemMetrics{
			TotalRatings:       0,
			TotalDisputes:      0,
			DisputesUpheld:     0,
			DisputesOverturned: 0,
			TotalStakeSlashed:  0,
			TotalActors:        0,
			LastUpdated:        time.Now().Unix(),
		}
	}
	
	switch metricType {
	case "rating":
		metrics.TotalRatings++
	case "dispute":
		metrics.TotalDisputes++
	case "upheld":
		metrics.DisputesUpheld++
	case "overturned":
		metrics.DisputesOverturned++
	case "slash":
		metrics.TotalStakeSlashed += MinStakeRequired * SlashPercentage
	}
	
	metrics.LastUpdated = time.Now().Unix()
	return putState(ctx, key, &metrics)
}

func (rc *ReputationContract) logPerformance(
	ctx contractapi.TransactionContextInterface,
	txID string,
	operation string,
	latency int64,
	payloadSize int,
) {
	log := PerformanceLog{
		TxID:        txID,
		Operation:   operation,
		Latency:     latency,
		PayloadSize: payloadSize,
		Timestamp:   time.Now().Unix(),
	}
	
	key, _ := performanceKey(ctx, txID)
	putState(ctx, key, &log)
}

// GetSystemMetrics - Query overall system statistics
func (rc *ReputationContract) GetSystemMetrics(
	ctx contractapi.TransactionContextInterface,
) (*SystemMetrics, error) {
	key, _ := metricsKey(ctx)
	var metrics SystemMetrics
	if err := getState(ctx, key, &metrics); err != nil {
		return &SystemMetrics{}, nil
	}
	return &metrics, nil
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func generateRatingID(raterID, actorID, dimension string, timestamp int64) string {
	data := fmt.Sprintf("%s:%s:%s:%d", raterID, actorID, dimension, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("RAT-%x", hash[:8])
}

func generateDisputeID(ratingID, challengerID string) string {
	data := fmt.Sprintf("%s:%s:%d", ratingID, challengerID, time.Now().Unix())
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("DIS-%x", hash[:8])
}

func calculateWilsonCI(alpha, beta, confidence float64) [2]float64 {
	// Wilson score interval for Beta distribution
	// Simplified approximation
	n := alpha + beta
	p := alpha / n
	
	if n < 2 {
		return [2]float64{0, 1}
	}
	
	z := 1.96 // 95% confidence
	denominator := 1 + z*z/n
	centre := (p + z*z/(2*n)) / denominator
	spread := z * math.Sqrt((p*(1-p)/n)+(z*z/(4*n*n))) / denominator
	
	lower := centre - spread
	upper := centre + spread
	
	if lower < 0 {
		lower = 0
	}
	if upper > 1 {
		upper = 1
	}
	
	return [2]float64{lower, upper}
}

// ============================================================================
// QUERY FUNCTIONS FOR KPI ANALYSIS
// ============================================================================

// GetRatingHistory - Get all ratings for an actor (for analysis)
func (rc *ReputationContract) GetRatingHistory(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
) ([]*Rating, error) {
	// This would use rich queries in CouchDB or iterator in production
	// Simplified for demonstration
	iterator, err := ctx.GetStub().GetStateByPartialCompositeKey(prefixRating, []string{})
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	
	var ratings []*Rating
	for iterator.HasNext() {
		queryResponse, err := iterator.Next()
		if err != nil {
			return nil, err
		}
		
		var rating Rating
		if err := json.Unmarshal(queryResponse.Value, &rating); err != nil {
			continue
		}
		
		if rating.ActorID == actorID && rating.Dimension == dimension {
			ratings = append(ratings, &rating)
		}
	}
	
	return ratings, nil
}

// GetPerformanceStats - Get performance statistics for benchmarking
func (rc *ReputationContract) GetPerformanceStats(
	ctx contractapi.TransactionContextInterface,
) (map[string]interface{}, error) {
	iterator, err := ctx.GetStub().GetStateByPartialCompositeKey(prefixPerformance, []string{})
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	
	var totalLatency int64
	var count int64
	var totalPayload int
	
	for iterator.HasNext() {
		queryResponse, err := iterator.Next()
		if err != nil {
			continue
		}
		
		var log PerformanceLog
		if err := json.Unmarshal(queryResponse.Value, &log); err != nil {
			continue
		}
		
		totalLatency += log.Latency
		totalPayload += log.PayloadSize
		count++
	}
	
	avgLatency := float64(0)
	avgPayload := float64(0)
	if count > 0 {
		avgLatency = float64(totalLatency) / float64(count)
		avgPayload = float64(totalPayload) / float64(count)
	}
	
	return map[string]interface{}{
		"avgLatencyUs":        avgLatency,
		"avgPayloadBytes":     avgPayload,
		"totalTransactions":   count,
	}, nil
}
// GetAttackEvents - Get detected attack events for analysis
func (rc *ReputationContract) GetAttackEvents(
	ctx contractapi.TransactionContextInterface,
) ([]*AttackEvent, error) {
	iterator, err := ctx.GetStub().GetStateByPartialCompositeKey(prefixAttackEvent, []string{})
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	
	var events []*AttackEvent
	for iterator.HasNext() {
		queryResponse, err := iterator.Next()
		if err != nil {
			continue
		}
		
		var event AttackEvent
		if err := json.Unmarshal(queryResponse.Value, &event); err != nil {
			continue
		}
		
		events = append(events, &event)
	}
	
	return events, nil
}

// GetDisputeStats - Get dispute resolution statistics
func (rc *ReputationContract) GetDisputeStats(
	ctx contractapi.TransactionContextInterface,
) (map[string]interface{}, error) {
	iterator, err := ctx.GetStub().GetStateByPartialCompositeKey(prefixDispute, []string{})
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	
	var totalDisputes int64
	var resolved int64
	var upheld int64
	var overturned int64
	var totalResolutionTime int64
	
	for iterator.HasNext() {
		queryResponse, err := iterator.Next()
		if err != nil {
			continue
		}
		
		var dispute Dispute
		if err := json.Unmarshal(queryResponse.Value, &dispute); err != nil {
			continue
		}
		
		totalDisputes++
		if dispute.Status == "resolved" {
			resolved++
			if dispute.Verdict == "upheld" {
				upheld++
			} else if dispute.Verdict == "overturned" {
				overturned++
			}
			
			resolutionTime := dispute.ResolvedTs - dispute.Timestamp
			totalResolutionTime += resolutionTime
		}
	}
	
	avgResolutionTime := float64(0)
	if resolved > 0 {
		avgResolutionTime = float64(totalResolutionTime) / float64(resolved)
	}
	
	return map[string]interface{}{
		"totalDisputes":       totalDisputes,
		"resolved":            resolved,
		"upheld":              upheld,
		"overturned":          overturned,
		"avgResolutionTimeSec": avgResolutionTime,
		"upheldRate":          float64(upheld) / float64(resolved),
		"overturnedRate":      float64(overturned) / float64(resolved),
	}, nil
}

// ============================================================================
// ADVANCED QUERIES FOR MARL ANALYSIS
// ============================================================================

// GetAgentProfile - Get comprehensive profile for MARL agent modeling
func (rc *ReputationContract) GetAgentProfile(
	ctx contractapi.TransactionContextInterface,
	actorID string,
) (map[string]interface{}, error) {
	dimensions := []string{"quality", "delivery", "compliance", "warranty"}
	
	profile := make(map[string]interface{})
	profile["actorId"] = actorID
	
	reputations := make(map[string]interface{})
	for _, dim := range dimensions {
		rep, err := rc.GetReputation(ctx, actorID, dim)
		if err == nil {
			reputations[dim] = rep
		}
	}
	profile["reputations"] = reputations
	
	// Get stake info
	stake, _ := rc.getOrInitStake(ctx, actorID)
	profile["stake"] = map[string]interface{}{
		"total":    stake.Amount,
		"locked":   stake.LockedAmount,
		"available": stake.Amount - stake.LockedAmount,
	}
	
	// Count ratings given and received
	ratingsGiven := 0
	ratingsReceived := 0
	
	iterator, _ := ctx.GetStub().GetStateByPartialCompositeKey(prefixRating, []string{})
	defer iterator.Close()
	
	for iterator.HasNext() {
		queryResponse, _ := iterator.Next()
		var rating Rating
		if json.Unmarshal(queryResponse.Value, &rating) == nil {
			if rating.RaterID == actorID {
				ratingsGiven++
			}
			if rating.ActorID == actorID {
				ratingsReceived++
			}
		}
	}
	
	profile["ratingsGiven"] = ratingsGiven
	profile["ratingsReceived"] = ratingsReceived
	
	// Get dispute involvement
	disputesInitiated := 0
	disputesAgainst := 0
	
	dispIterator, _ := ctx.GetStub().GetStateByPartialCompositeKey(prefixDispute, []string{})
	defer dispIterator.Close()
	
	for dispIterator.HasNext() {
		queryResponse, _ := dispIterator.Next()
		var dispute Dispute
		if json.Unmarshal(queryResponse.Value, &dispute) == nil {
			if dispute.Challenger == actorID {
				disputesInitiated++
			}
			// Check if dispute is against this actor's rating
			rKey, _ := ratingKey(ctx, dispute.RatingID)
			var rating Rating
			if getState(ctx, rKey, &rating) == nil {
				if rating.ActorID == actorID {
					disputesAgainst++
				}
			}
		}
	}
	
	profile["disputesInitiated"] = disputesInitiated
	profile["disputesAgainst"] = disputesAgainst
	
	return profile, nil
}

// BatchGetReputations - Efficient batch query for MARL state
func (rc *ReputationContract) BatchGetReputations(
	ctx contractapi.TransactionContextInterface,
	actorIDs string, // Comma-separated list
	dimension string,
) ([]map[string]interface{}, error) {
	ids := strings.Split(actorIDs, ",")
	results := make([]map[string]interface{}, 0)
	
	for _, actorID := range ids {
		actorID = trim(actorID)
		if actorID == "" {
			continue
		}
		
		rep, err := rc.GetReputation(ctx, actorID, dimension)
		if err == nil {
			results = append(results, rep)
		}
	}
	
	return results, nil
}

// SimulateRatingImpact - Predict reputation change without committing
// Useful for MARL agents to plan actions
func (rc *ReputationContract) SimulateRatingImpact(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
	valueStr string,
) (map[string]interface{}, error) {
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return nil, errors.New("invalid value")
	}
	
	// Get current state
	currentRep, err := rc.GetReputation(ctx, actorID, dimension)
	if err != nil {
		return nil, err
	}
	
	// Get rater weight
	raterID, _ := ctx.GetClientIdentity().GetID()
	weight, _ := rc.calculateRaterWeight(ctx, raterID, dimension)
	
	// Simulate update
	alpha := currentRep["alpha"].(float64)
	beta := currentRep["beta"].(float64)
	
	if value >= 0.5 {
		alpha += weight * value
	} else {
		beta += weight * (1.0 - value)
	}
	
	newScore := alpha / (alpha + beta)
	oldScore := currentRep["score"].(float64)
	
	return map[string]interface{}{
		"currentScore": oldScore,
		"predictedScore": newScore,
		"scoreDelta": newScore - oldScore,
		"raterWeight": weight,
	}, nil
}

// ============================================================================
// TESTING AND DEBUGGING FUNCTIONS
// ============================================================================

// ResetReputation - FOR TESTING ONLY - Reset an actor's reputation
func (rc *ReputationContract) ResetReputation(
	ctx contractapi.TransactionContextInterface,
	actorID string,
	dimension string,
) error {
	// WARNING: Should be disabled in production or restricted to admin only
	
	rep := &Reputation{
		ActorID:     actorID,
		Dimension:   dimension,
		Alpha:       1.0,
		Beta:        1.0,
		LastTs:      time.Now().Unix(),
		TotalEvents: 0,
	}
	
	key, _ := reputationKey(ctx, actorID, dimension)
	return putState(ctx, key, rep)
}

// GetAllActors - List all actors with reputation records
func (rc *ReputationContract) GetAllActors(
	ctx contractapi.TransactionContextInterface,
) ([]string, error) {
	iterator, err := ctx.GetStub().GetStateByPartialCompositeKey(prefixReputation, []string{})
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	
	actorSet := make(map[string]bool)
	for iterator.HasNext() {
		queryResponse, err := iterator.Next()
		if err != nil {
			continue
		}
		
		var rep Reputation
		if err := json.Unmarshal(queryResponse.Value, &rep); err != nil {
			continue
		}
		
		actorSet[rep.ActorID] = true
	}
	
	actors := make([]string, 0, len(actorSet))
	for actor := range actorSet {
		actors = append(actors, actor)
	}
	
	return actors, nil
}

// ============================================================================
// MAIN
// ============================================================================

