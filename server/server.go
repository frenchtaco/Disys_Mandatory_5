// server/server.go
package main

import (
	proto "Auction/grpc"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Auction duration: 100 time units => 100 seconds here for simplicity.
const AuctionDuration = 100 * time.Second

type AuctionServer struct {
	proto.UnimplementedAuctionServiceServer
	proto.UnimplementedInternalServiceServer

	mu             sync.Mutex
	startTime      time.Time
	duration       time.Duration
	closed         bool
	bids           map[string]uint32 // highest bid per bidder
	winner         string
	winningAmount  uint32
	currentHighest uint32

	isLeader     bool
	replicaAddrs []string // only used if isLeader == true
}

func NewAuctionServer(isLeader bool, replicaAddrs []string) *AuctionServer {
	return &AuctionServer{
		startTime:    time.Now(),
		duration:     AuctionDuration,
		bids:         make(map[string]uint32),
		isLeader:     isLeader,
		replicaAddrs: replicaAddrs,
	}
}

// Bid is called by clients. Only the leader accepts bids.
// Backups reject Bid from clients to keep logic simple and clearly leader-based.
func (s *AuctionServer) Bid(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {
	if !s.isLeader {
		return &proto.BidResponse{
			Outcome: proto.BidResponse_FAIL,
			Message: "this node is a backup; send bids to the leader",
		}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Close auction if time is up.
	s.maybeCloseAuctionLocked()

	if s.closed {
		return &proto.BidResponse{
			Outcome: proto.BidResponse_FAIL,
			Message: "auction is closed",
		}, nil
	}

	bidder := req.Bidder
	amount := req.Amount
	if bidder == "" {
		return &proto.BidResponse{
			Outcome: proto.BidResponse_FAIL,
			Message: "bidder name cannot be empty",
		}, nil
	}
	if amount == 0 {
		return &proto.BidResponse{
			Outcome: proto.BidResponse_FAIL,
			Message: "bid amount must be > 0",
		}, nil
	}

	// Auction semantics:
	// 1) First bid registers the bidder (implicitly).
	// 2) New bids from same bidder must be higher than previous.
	// 3) We also require bid > currentHighest to become new leader.
	prev := s.bids[bidder]
	if amount <= prev {
		return &proto.BidResponse{
			Outcome: proto.BidResponse_FAIL,
			Message: fmt.Sprintf("new bid must be higher than previous bid (%d)", prev),
		}, nil
	}
	if amount <= s.currentHighest {
		return &proto.BidResponse{
			Outcome: proto.BidResponse_FAIL,
			Message: fmt.Sprintf("bid must exceed current highest (%d)", s.currentHighest),
		}, nil
	}

	// Save old state for potential rollback on replication failure.
	oldPrev := prev
	oldWinner := s.winner
	oldWinning := s.winningAmount
	oldHighest := s.currentHighest

	// Apply locally on leader.
	s.bids[bidder] = amount
	s.currentHighest = amount
	s.winner = bidder
	s.winningAmount = amount

	// Replicate to backups while holding the lock to preserve order.
	if err := s.replicateToBackupsLocked(ctx, req); err != nil {
		// Roll back to old state if replication fails.
		if oldPrev == 0 {
			delete(s.bids, bidder)
		} else {
			s.bids[bidder] = oldPrev
		}
		s.winner = oldWinner
		s.winningAmount = oldWinning
		s.currentHighest = oldHighest

		log.Printf("replication failed: %v", err)
		return &proto.BidResponse{
			Outcome: proto.BidResponse_EXCEPTION,
			Message: "replication failed; bid not accepted",
		}, nil
	}

	return &proto.BidResponse{
		Outcome: proto.BidResponse_SUCCESS,
		Message: fmt.Sprintf("bid accepted: %s bid %d", bidder, amount),
	}, nil
}

// Result can be called on any node (leader or backup).
func (s *AuctionServer) Result(ctx context.Context, _ *proto.ResultRequest) (*proto.ResultResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.maybeCloseAuctionLocked()

	return &proto.ResultResponse{
		Closed:         s.closed,
		Winner:         s.winner,
		WinningAmount:  s.winningAmount,
		CurrentHighest: s.currentHighest,
	}, nil
}

// Replicate is called only by the leader on backup nodes.
// It applies the accepted bid state without further replication.
func (s *AuctionServer) Replicate(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Even on backups we keep track of closure, but we trust the leader's decisions.
	s.maybeCloseAuctionLocked()

	// Apply the leader's decision directly.
	bidder := req.Bidder
	amount := req.Amount

	if bidder == "" || amount == 0 {
		// Should never happen if leader is correct; treat as exception but still avoid panicking.
		return &proto.BidResponse{
			Outcome: proto.BidResponse_EXCEPTION,
			Message: "invalid replication payload",
		}, nil
	}

	// Overwrite with leader's state.
	s.bids[bidder] = amount
	if amount >= s.currentHighest {
		s.currentHighest = amount
		s.winner = bidder
		s.winningAmount = amount
	}

	return &proto.BidResponse{
		Outcome: proto.BidResponse_SUCCESS,
		Message: "replicated",
	}, nil
}

func (s *AuctionServer) maybeCloseAuctionLocked() {
	if !s.closed && time.Since(s.startTime) >= s.duration {
		s.closed = true
	}
}

// This is called under s.mu to preserve bid ordering between leader and backups.
func (s *AuctionServer) replicateToBackupsLocked(ctx context.Context, req *proto.BidRequest) error {
	if !s.isLeader {
		return nil
	}
	// Simple, synchronous replication to all configured backups.
	for _, addr := range s.replicaAddrs {
		if strings.TrimSpace(addr) == "" {
			continue
		}
		conn, err := grpc.DialContext(
			ctx,
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		if err != nil {
			return fmt.Errorf("dial replica %s: %w", addr, err)
		}

		client := proto.NewInternalServiceClient(conn)
		_, err = client.Replicate(ctx, req)
		conn.Close()
		if err != nil {
			return fmt.Errorf("replicate to %s: %w", addr, err)
		}
	}
	return nil
}

func main() {
	port := flag.String("port", ":5000", "address to listen on (e.g. :5000)")
	isLeader := flag.Bool("leader", false, "run this node as leader")
	replicasStr := flag.String("replicas", "", "comma-separated list of backup addresses (only used on leader)")

	flag.Parse()

	var replicas []string
	if *isLeader && *replicasStr != "" {
		parts := strings.Split(*replicasStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				replicas = append(replicas, p)
			}
		}
	}

	srv := NewAuctionServer(*isLeader, replicas)

	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *port, err)
	}

	grpcServer := grpc.NewServer()
	proto.RegisterAuctionServiceServer(grpcServer, srv)
	proto.RegisterInternalServiceServer(grpcServer, srv)

	role := "backup"
	if *isLeader {
		role = "leader"
	}
	log.Printf("Auction node started on %s (%s). Auction duration: %s", *port, role, AuctionDuration)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server terminated: %v", err)
	}
}
