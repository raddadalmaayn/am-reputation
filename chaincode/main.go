package main

import (
  "github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

type ReputationContract struct {
  contractapi.Contract
}

func main() {
  cc, err := contractapi.NewChaincode(new(ReputationContract))
  if err != nil {
    panic(err)
  }
  if err := cc.Start(); err != nil {
    panic(err)
  }
}
