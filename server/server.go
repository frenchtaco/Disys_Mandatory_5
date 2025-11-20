// server/server.go
package main

import (
	proto "Auction/grpc"
	//"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	//"google.golang.org/grpc/credentials/insecure"
)

// Auction duration: 100 time units => 100 seconds here for simplicity.
//const AuctionDuration = 100 * time.Second

type AuctionServer struct {
	proto.UnimplementedAuctionServiceServer
	proto.UnimplementedInternalServiceServer

	mu             sync.Mutex
	closed         bool
	bidders        map[string]proto.AuctionService_TrafficServer
	bids           map[string]uint32 // highest bid per bidder
	winner         string
	currentHighest uint32

	isLeader     bool
	replicaAddrs []string // only used if isLeader == true
	leaderAddr   string   // used by backups to forward bids to leader
}

func NewAuctionServer(isLeader bool, replicaAddrs []string, leaderAddr string) *AuctionServer {
	return &AuctionServer{
		closed:         false,
		bidders:        make(map[string]proto.AuctionService_TrafficServer),
		bids:           make(map[string]uint32),
		winner:         "",
		currentHighest: 0,
		isLeader:       isLeader,
		replicaAddrs:   replicaAddrs,
		leaderAddr:     leaderAddr,
	}
}

// Bid is called by clients. Leader accepts bids and replicates them.
// Backups forward Bid to the leader so clients can talk to any node.
func (s *AuctionServer) Traffic(stream proto.AuctionService_TrafficServer) error {
	var username string

	if !s.isLeader {
		s.isLeader = true
	}

	for {
		in, err := stream.Recv()

		if err != nil {
			fmt.Printf("Bidder has disconnected: %s\n", username)
			s.removeBidder(username)
			return nil
		}
		if username == "" {
			username = in.Username
			s.addBidder(username, stream)
		}

		if s.closed {
			msg := &proto.AuctionMessage{
				Username: s.winner,
				Type:     "result",
				Amount:   s.currentHighest,
			}
			stream.Send(msg)
		}

		if in.Type == "bid" {
			s.mu.Lock()
			if in.Amount <= s.currentHighest {
				msg := &proto.AuctionMessage{
					Username: s.winner,
					Type:     "bid",
					Amount:   s.currentHighest,
				}
				stream.Send(msg)
			}

			s.winner = in.Username
			s.currentHighest = in.Amount

			s.bids[username] = s.currentHighest

			msg := &proto.AuctionMessage{
				Username: s.winner,
				Type:     "bid",
				Amount:   s.currentHighest,
			}
			s.announce(msg)

			s.mu.Unlock()
		}

		if in.Type == "result" {

			msg := &proto.AuctionMessage{
				Username: s.winner,
				Type:     "bid",
				Amount:   s.currentHighest,
			}
			stream.Send(msg)
		}
	}
}



func (s *AuctionServer) addBidder(username string, stream proto.AuctionService_TrafficServer) {
	s.mu.Lock()
	s.bidders[username] = stream
	s.mu.Unlock()
}

func (s *AuctionServer) removeBidder(username string) {
	s.mu.Lock()
	delete(s.bidders, username)
	s.mu.Unlock()
}

func (s *AuctionServer) announce(msg *proto.AuctionMessage) {
	for _, stream := range s.bidders {
		stream.Send(msg)
	}
}

// 	bidder := req.Bidder
// 	amount := req.Amount
// 	if bidder == "" {
// 		return &proto.BidResponse{
// 			Outcome: proto.BidResponse_FAIL,
// 			Message: "bidder name cannot be empty",
// 		}, nil
// 	}
// 	if amount == 0 {
// 		return &proto.BidResponse{
// 			Outcome: proto.BidResponse_FAIL,
// 			Message: "bid amount must be > 0",
// 		}, nil
// 	}

// 	// Auction semantics:
// 	// 1) First bid registers the bidder (implicitly).
// 	// 2) New bids from same bidder must be higher than previous.
// 	// 3) We also require bid > currentHighest to become new leader.
// 	prev := s.bids[bidder]
// 	if amount <= prev {
// 		return &proto.BidResponse{
// 			Outcome: proto.BidResponse_FAIL,
// 			Message: fmt.Sprintf("new bid must be higher than previous bid (%d)", prev),
// 		}, nil
// 	}
// 	if amount <= s.currentHighest {
// 		return &proto.BidResponse{
// 			Outcome: proto.BidResponse_FAIL,
// 			Message: fmt.Sprintf("bid must exceed current highest (%d)", s.currentHighest),
// 		}, nil
// 	}

// 	// Save old state for potential rollback on replication failure.
// 	oldPrev := prev
// 	oldWinner := s.winner
// 	oldWinning := s.winningAmount
// 	oldHighest := s.currentHighest

// 	// Apply locally on leader.
// 	s.bids[bidder] = amount
// 	s.currentHighest = amount
// 	s.winner = bidder
// 	s.winningAmount = amount

// 	// Replicate to backups while holding the lock to preserve order.
// 	if err := s.replicateToBackupsLocked(ctx, req); err != nil {
// 		// Roll back to old state if replication fails.
// 		if oldPrev == 0 {
// 			delete(s.bids, bidder)
// 		} else {
// 			s.bids[bidder] = oldPrev
// 		}
// 		s.winner = oldWinner
// 		s.winningAmount = oldWinning
// 		s.currentHighest = oldHighest

// 		log.Printf("replication failed: %v", err)
// 		return &proto.BidResponse{
// 			Outcome: proto.BidResponse_EXCEPTION,
// 			Message: "replication failed; bid not accepted",
// 		}, nil
// 	}

// 	return &proto.BidResponse{
// 		Outcome: proto.BidResponse_SUCCESS,
// 		Message: fmt.Sprintf("bid accepted: %s bid %d", bidder, amount),
// 	}, nil
//

// Result can be called on any node (leader or backup).
// func (s *AuctionServer) Result(ctx context.Context, _ *proto.ResultRequest) (*proto.ResultResponse, error) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	s.maybeCloseAuctionLocked()

// 	return &proto.ResultResponse{
// 		Closed:         s.closed,
// 		Winner:         s.winner,
// 		WinningAmount:  s.winningAmount,
// 		CurrentHighest: s.currentHighest,
// 	}, nil
// }

// Replicate is called only by the leader on backup nodes.
// It applies the accepted bid state without further replication.
// func (s *AuctionServer) Replicate(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	// Even on backups we keep track of closure, but we trust the leader's decisions.
// 	s.maybeCloseAuctionLocked()

// 	// Apply the leader's decision directly.
// 	bidder := req.Bidder
// 	amount := req.Amount

// 	if bidder == "" || amount == 0 {
// 		// Should never happen if leader is correct; treat as exception but still avoid panicking.
// 		return &proto.BidResponse{
// 			Outcome: proto.BidResponse_EXCEPTION,
// 			Message: "invalid replication payload",
// 		}, nil
// 	}

// 	// Overwrite with leader's state.
// 	s.bids[bidder] = amount
// 	if amount >= s.currentHighest {
// 		s.currentHighest = amount
// 		s.winner = bidder
// 		s.winningAmount = amount
// 	}

// 	return &proto.BidResponse{
// 		Outcome: proto.BidResponse_SUCCESS,
// 		Message: "replicated",
// 	}, nil
// }

// func (s *AuctionServer) maybeCloseAuctionLocked() {
// 	if !s.closed && time.Since(s.startTime) >= s.duration {
// 		s.closed = true
// 	}
// }

// This is called under s.mu to preserve bid ordering between leader and backups.
// This is called under s.mu to preserve bid ordering between leader and backups.
// func (s *AuctionServer) replicateToBackupsLocked(ctx context.Context, req *proto.BidRequest) error {
// 	if !s.isLeader {
// 		return nil
// 	}

// 	if len(s.replicaAddrs) == 0 {
// 		// Nothing to replicate; accept bid on leader only.
// 		return nil
// 	}

// 	successes := 0
// 	var lastErr error

// 	// Simple, synchronous replication to all configured backups.
// 	for _, addr := range s.replicaAddrs {
// 		if strings.TrimSpace(addr) == "" {
// 			continue
// 		}
// 		conn, err := grpc.DialContext(
// 			ctx,
// 			addr,
// 			grpc.WithTransportCredentials(insecure.NewCredentials()),
// 			grpc.WithBlock(),
// 		)
// 		if err != nil {
// 			log.Printf("replication: could not dial replica %s: %v", addr, err)
// 			lastErr = err
// 			continue
// 		}

// 		client := proto.NewInternalServiceClient(conn)
// 		_, err = client.Replicate(ctx, req)
// 		conn.Close()
// 		if err != nil {
// 			log.Printf("replication: Replicate to %s failed: %v", addr, err)
// 			lastErr = err
// 			continue
// 		}
// 		successes++
// 	}

// 	// Require at least one successful replication (if replicas exist).
// 	if successes == 0 {
// 		if lastErr != nil {
// 			return fmt.Errorf("replication failed on all replicas: %w", lastErr)
// 		}
// 		return fmt.Errorf("replication failed on all replicas")
// 	}

// 	return nil
// }

func main() {
	port := flag.String("port", ":5000", "address to listen on (e.g. :5000)")
	isLeader := flag.Bool("leader", false, "run this node as leader")
	replicasStr := flag.String("replicas", "", "comma-separated list of backup addresses (only used on leader)")
	leaderAddrFlag := flag.String("leader_addr", "localhost:5000", "leader address used by backups to forward bids")
	duration := flag.Int("duration", 0000, "duration of the auction")

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

	// For the leader itself, leaderAddr is not used for forwarding, but we can still set it.
	srv := NewAuctionServer(*isLeader, replicas, *leaderAddrFlag)

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

	log.Printf("Auction node started on %s (%s). Auction duration: %d", *port, role, *duration)

	go func() {
		for i := 0; i < *duration; i++ {
			time.Sleep(1 * time.Second)
		}
		srv.closed = true
	}()
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server terminated: %v", err)
	}
}
