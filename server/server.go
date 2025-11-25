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

// * Replica Manager
type ReplicaManager struct {
	proto.UnimplementedAuctionServiceServer
	mu              sync.Mutex
	bidCollection   map[string]uint32                             // A collection of bidders and their bids
	attendees       []proto.AuctionService_BroadcastResultsServer // A collection of bidder
	highestBid      uint32
	highestBidder   string
	isAuctionClosed bool
	auctionStart    int64
	auctionDuration int64
	auctionCancel   context.CancelFunc
	portNumber      uint32
}

// * Simple helper function to create a new RM:
func NewReplicaManager(port uint32) *ReplicaManager {
	return &ReplicaManager{
		bidCollection:   make(map[string]uint32),
		mu:              sync.Mutex{},
		highestBid:      0,
		highestBidder:   "",
		isAuctionClosed: true,
		portNumber:      port,
	}
}

func main() {
	arg, _ := strconv.ParseInt(os.Args[1], 10, 32)
	ownPort := uint32(arg) + 5067

	// * Create a new replica manager:
	RM := NewReplicaManager(ownPort)

	// * Start the RM server:
	RM.Start()
}

func (RM *ReplicaManager) Start() {
	RMServer := grpc.NewServer()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", RM.portNumber))

	if err != nil {
		log.Fatalf("Attempted to establish server on port: %v --- Error Message: %v", RM.portNumber, err)
	} else {
		log.Printf("Successfully established server on port: %v", RM.portNumber)
	}

	proto.RegisterAuctionServiceServer(RMServer, RM)
	serverErr := RMServer.Serve(listener)
	if serverErr != nil {
		log.Fatalf("Could not serve the specified listener: %v", serverErr)
	}
}

func (RM *ReplicaManager) Bid(ctx context.Context, bidMsg *proto.T_Bid) (*proto.T_BidResponse, error) {
	RM.mu.Lock()
	defer RM.mu.Unlock()

	// * Auction timer already ran out:
	if RM.isAuctionClosed {
		return &proto.T_BidResponse{Status: proto.BID_STATUS_BID_FAILED}, nil
	}

	// * Server failure - pinpoint why:
	res, err := RM.GetHighestBid(ctx, &proto.Empty{})
	if err != nil {
		log.Printf("GetHighestBid error: %v", err)
		return &proto.T_BidResponse{Status: proto.BID_STATUS_BID_FAILED}, nil
	}

	// * Bid was accepted:
	if bidMsg.Amount > res.HighestBid {
		RM.bidCollection[bidMsg.Bidder] = bidMsg.Amount
		// update cached highest if you keep one:
		RM.highestBid = bidMsg.Amount
		RM.highestBidder = bidMsg.Bidder
		return &proto.T_BidResponse{Status: proto.BID_STATUS_BID_SUCCESS}, nil
	}

	// * Bid was too small:
	return &proto.T_BidResponse{Status: proto.BID_STATUS_BID_TOO_LOW}, nil
}

func (RM *ReplicaManager) GetHighestBid(ctx context.Context, empty *proto.Empty) (*proto.T_Result, error) {

	currHighestBidder := ""
	var currHighestBid uint32 = 0

	for bidder, bid := range RM.bidCollection {
		if bid > currHighestBid {
			currHighestBid = bid
			currHighestBidder = bidder
		}
	}

	return &proto.T_Result{HighestBid: currHighestBid, HighestBidder: currHighestBidder}, nil
}

func (RM *ReplicaManager) StartAuction(ctx context.Context, req *proto.T_StartAucReq) (*proto.Empty, error) {
	RM.mu.Lock()
	defer RM.mu.Unlock()

	// * If auction has already started - leave:
	if !RM.isAuctionClosed {
		return &proto.Empty{}, nil
	}

	RM.isAuctionClosed = false
	RM.auctionStart = req.AuctionStart
	RM.auctionDuration = req.AuctionDuration

	// * Calculate the endtime by adding the start time to the auction duration time:
	endTime := time.Unix(0, RM.auctionStart).Add(time.Duration(RM.auctionDuration))

	log.Printf("Auction has officially started!")

	go func() {
		time.Sleep(time.Until(endTime))
		RM.mu.Lock()
		RM.isAuctionClosed = true
		RM.mu.Unlock()
	}()

	return &proto.Empty{}, nil
}

func (RM *ReplicaManager) BroadcastResults(empty *proto.Empty, stream proto.AuctionService_BroadcastResultsServer) error {
	RM.mu.Lock()
	RM.attendees = append(RM.attendees, stream)
	RM.mu.Unlock()

	// * We enter an "indefinite" loop which just continuously
	for {
		time.Sleep(500 * time.Millisecond)
		RM.mu.Lock()
		closed := RM.isAuctionClosed
		RM.mu.Unlock()
		if closed {
			return stream.Send(&proto.T_Result{
				HighestBidder: RM.highestBidder,
				HighestBid:    RM.highestBid,
			})
		}
	}
}
