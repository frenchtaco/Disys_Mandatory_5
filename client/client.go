package main

import (
	proto "Auction/grpc"
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//////////////////////////////////////////////////
//                                              //
//                   STRUCTS                    //
//                                              //
//////////////////////////////////////////////////

const (
	auctionDuration = 20 * time.Second // "100 time units" interpreted here as seconds; configurable
)

type Bidder struct {
	name string
}

//   - Frontend acts as this bridge between the bidders (clients) and
//     auction servers (replica managers):
type Frontend struct {
	id                string
	RMPorts           []int32
	auctionBidders    []proto.AuctionServiceClient
	hasAuctionStarted bool
}

// * Main loop:
func main() {
	bidderName := os.Args[1]
	bidder := &Bidder{
		name: bidderName,
	}

	// TODO: Change this to work alongside the Shell file...
	frontend := &Frontend{
		id:                bidderName,
		RMPorts:           []int32{5067, 5068, 5069},
		auctionBidders:    []proto.AuctionServiceClient{},
		hasAuctionStarted: false,
	}

	successPorts, failPorts, err := frontend.ConnectToFrontend()

	if err != nil {
		log.Printf("ConnectToFrontend reported error: %v\n", err)
	}

	log.Printf("Successfully connected ports: %v\n", successPorts)
	log.Printf("Failed to connect ports: %v\n", failPorts)

	go QueryBidder(bidder, frontend)

	select {}
}

func (FE *Frontend) ConnectToFrontend() ([]int32, []int32, error) {
	var successPorts []int32
	var failPorts []int32

	for _, port := range FE.RMPorts {
		// ORIGINAL: conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))

		addr := "localhost:" + strconv.Itoa(int(port))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())

		// This is called to clean-up the timer and goroutine left behind by calling "context.WithTimeout"
		// Outside a for-loop, it is called in conjunction with 'defer', inside however, it is called immediately.
		cancel()

		if err != nil {
			log.Printf("Unable to connect to port: %d", port)
			failPorts = append(failPorts, port)
			continue
		}

		log.Printf("Successfully connected to port: %d", port)
		FE.auctionBidders = append(FE.auctionBidders, proto.NewAuctionServiceClient(conn))
		successPorts = append(successPorts, port)
	}

	if len(successPorts) == 0 {
		return successPorts, failPorts, fmt.Errorf("no replica managers connected")
	}

	return successPorts, failPorts, nil
}

// * QueryBidder will continuously check the user for any input:
func QueryBidder(bidder *Bidder, frontend *Frontend) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		scanVal := scanner.Text()
		if strings.HasPrefix(scanVal, "bid") {
			bidAmount, err := strconv.Atoi(strings.Split(scanVal, " ")[1])
			if err != nil {
				log.Println("Bid was not a number. Please input a number.")
				continue
			}
			bidder.Bid(uint32(bidAmount), frontend)

		} else if strings.HasPrefix(scanVal, "result") {
			bidder.GetAuctionResult(frontend)
		}
	}
}

//////////////////////////////////////////////////
//                                              //
//                   MAKE A BID                 //
//                                              //
//////////////////////////////////////////////////

// * [BIDDER] Make a bid:
func (bidder *Bidder) Bid(bidAmount uint32, FE *Frontend) {
	FE.Bid(bidder.name, bidAmount)
	log.Printf("You bid %d€\n", bidAmount)
}

// * [FRONTEND] Make a bid:
func (FE *Frontend) Bid(bidderName string, bidAmount uint32) {

	// * On the first ever
	if !FE.hasAuctionStarted {
		FE.hasAuctionStarted = true
		startTime := time.Now().UnixNano()
		duration := auctionDuration.Nanoseconds()

		for _, rm := range FE.auctionBidders {
			_, err := rm.StartAuction(context.Background(), &proto.T_StartAucReq{
				AuctionStart:    startTime,
				AuctionDuration: duration,
				Initiator:       bidderName,
			})
			if err != nil {
				log.Printf("Failed to start auction on RM: %v", err)
			}
		}
	}

	// * Iterate over all the auction bidders and confirm:
	for _, auctionServiceClient := range FE.auctionBidders {
		res, err := auctionServiceClient.Bid(context.Background(), &proto.T_Bid{Bidder: bidderName, Amount: bidAmount})

		// * If an error occurred we cut that server away from the package of replica manager:
		if err != nil {
			FE.auctionBidders = RemoveFromArr(FE.auctionBidders, auctionServiceClient)
			log.Printf("Unable to bid bid %d for bidder: %s - Error: %s\n", bidAmount, bidderName, err.Error())
		} else {
			switch res.Status {
			case proto.BID_STATUS_BID_FAILED:
				log.Printf("Bid of %s failed due to server error.", bidderName)
			case proto.BID_STATUS_BID_TOO_LOW:
				log.Printf("Bid of %s failed as it was too low.", bidderName)
			case proto.BID_STATUS_BID_SUCCESS:
				log.Printf("Bid of %s successfully went through.", bidderName)
			default:
				log.Printf("Bid of %s failed as bid status message type was not recognized", bidderName)
			}
		}
	}
}

//////////////////////////////////////////////////
//                                              //
//              REQUESTING RESULTS              //
//                                              //
//////////////////////////////////////////////////

// * [BIDDER] Request for auction result:
func (bidder *Bidder) GetAuctionResult(FE *Frontend) {
	winningBidder, winningBid, err := FE.GetAuctionResult()
	if err != nil {
		log.Printf("Error getting auction result: %v", err)
	}

	log.Printf("The highest bid is: %v - Bid placed by: %s", winningBid, winningBidder)
}

// * [FRONTEND] Request for auction result:
func (FE *Frontend) GetAuctionResult() (string, uint32, error) {
	if len(FE.auctionBidders) == 0 {
		return "", 0, fmt.Errorf("no replica managers available")
	}

	res, err := FE.auctionBidders[0].GetHighestBid(context.Background(), &proto.Empty{})
	if err != nil {
		log.Printf("Unable to receive result from server: %v", err)
		FE.auctionBidders = RemoveFromArr(FE.auctionBidders, FE.auctionBidders[0])
		return FE.GetAuctionResult()
	}
	return res.HighestBidder, res.HighestBid, nil
}

func RemoveFromArr(arr []proto.AuctionServiceClient, targetVal proto.AuctionServiceClient) []proto.AuctionServiceClient {
	var result []proto.AuctionServiceClient
	for _, val := range arr {
		if val == nil || val == targetVal {
			continue
		}
		result = append(result, val)
	}
	return result
}

// Subscribing to auction:
func (bidder *Bidder) JoinAuctionAnnouncement(FE *Frontend) {
	for _, RM := range FE.auctionBidders {
		stream, err := RM.BroadcastResults(context.Background(), &proto.Empty{})
		if err != nil {
			log.Printf("Error when attempting to connect to showing of final results: %v", err)
			continue
		}

		// *
		go func(s proto.AuctionService_BroadcastResultsClient) {
			for {
				res, err := s.Recv()
				if err != nil {
					log.Printf("Stream closed: %v", err)
					return
				}
				log.Printf("Auction ended! Winner: %s, Amount: %d", res.HighestBidder, res.HighestBid)
			}
		}(stream)
	}
}
