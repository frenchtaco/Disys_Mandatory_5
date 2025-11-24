package main

import (
	proto "Auction/grpc"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
)

const (
	auctionDuration = 100 * time.Second // "100 time units" interpreted here as seconds; configurable
)

// ReplicaManager: Design based on figure 18.1 in the book
type ReplicaManager struct {
	proto.UnimplementedAuctionServiceServer
	mu              sync.Mutex
	bidCollection   map[string]uint32
	highestBid      uint32
	highestBidder   string
	isAuctionClosed bool
	portNumber      uint32 // Also ID
}

func NewReplicaManager(port uint32) *ReplicaManager {
	return &ReplicaManager{
		bidCollection:   make(map[string]uint32),
		mu:              sync.Mutex{},
		highestBid:      0,
		highestBidder:   "",
		isAuctionClosed: false,
		portNumber:      port,
	}
}

// Change this to take input from a shell file.
func main() {
	arg1, _ := strconv.ParseInt(os.Args[1], 10, 32)
	ownPort := uint32(arg1) + 5000

	RM := &ReplicaManager{
		portNumber:    ownPort,
		bidCollection: make(map[string]uint32),
	}

	RM.Start()
}

func (RM *ReplicaManager) Start() {
	RMServer := grpc.NewServer()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%v", RM.portNumber))

	if err != nil {
		log.Fatalf("Attempted to establish server on port %v - Error Message: %v", RM.portNumber, err)
	} else {
		log.Printf("Started Replication Manager at port: %d\n", RM.portNumber)
	}

	proto.RegisterAuctionServiceServer(RMServer, RM)
	serverErr := RMServer.Serve(listener)
	if serverErr != nil {
		log.Fatalf("Could not serve the specified listener: %v", serverErr)
	}
}

func (RM *ReplicaManager) Bid(ctx context.Context, bidMsg *proto.T_Bid) (*proto.T_Ack, error) {
	RM.mu.Lock()
	defer RM.mu.Unlock()

	if RM.isAuctionClosed {
		return &proto.T_Ack{BidStatus: proto.BID_STATUS_BID_FAILED}, nil
	}

	res, err := RM.GetHighestBid()
	if err != nil {
		// treat as failure
		log.Printf("GetHighestBid error: %v", err)
		return &proto.T_Ack{BidStatus: proto.BID_STATUS_BID_FAILED}, nil
	}

	// Accept if strictly greater than current highest
	if bidMsg.Amount > res.HighestBid {
		RM.bidCollection[bidMsg.Bidder] = bidMsg.Amount
		// update cached highest if you keep one:
		RM.highestBid = bidMsg.Amount
		RM.highestBidder = bidMsg.Bidder
		return &proto.T_Ack{BidStatus: proto.BID_STATUS_BID_SUCCESS}, nil
	}

	return &proto.T_Ack{BidStatus: proto.BID_STATUS_BID_TOO_LOW}, nil
}
func (RM *ReplicaManager) GetFinalHighestBid(ctx context.Context, empty *proto.Empty) (*proto.T_Res, error) {

	res, err := RM.GetHighestBid()

	return &proto.T_Res{HighestBid: res.HighestBid, HighestBidder: res.HighestBidder}, err
}

func (RM *ReplicaManager) GetHighestBid() (*proto.T_Res, error) {
	currHighestBidder := ""
	var currHighestBid uint32 = 0

	// iterate through local bidCollection
	for bidder, bid := range RM.bidCollection {
		if bid > currHighestBid {
			currHighestBid = bid
			currHighestBidder = bidder
		}
	}

	// if no bids yet, return with zero and nil error (or you can return a specific error if you prefer)
	return &proto.T_Res{HighestBid: currHighestBid, HighestBidder: currHighestBidder}, nil
}
