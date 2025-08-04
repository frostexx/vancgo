package util

import (
	"math/rand"
	"time"
)

// Competitive fees based on competitor analysis
var competitiveFees = []int64{
	3200000,  // 3.2M PI
	9400000,  // 9.4M PI  
	5000000,  // 5M PI
	7500000,  // 7.5M PI
	12000000, // 12M PI for extreme competition
}

func GetCompetitiveFee() int64 {
	// Use random high fee to compete with other bots
	rand.Seed(time.Now().UnixNano())
	return competitiveFees[rand.Intn(len(competitiveFees))]
}

func GetTransferFee() int64 {
	// High transfer fee for speed
	return 5000000 // 5M PI
}

func GetNetworkFloodFee() int64 {
	// Maximum fee for network flooding
	return 15000000 // 15M PI
}