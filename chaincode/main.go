package main

import (
	"log"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

func main() {
	cc, err := contractapi.NewChaincode(new(ReputationContract))
	if err != nil {
		log.Panicf("error creating chaincode: %v", err)
	}
	if err := cc.Start(); err != nil {
		log.Panicf("error starting chaincode: %v", err)
	}
}
