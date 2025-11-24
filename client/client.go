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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TODO: Add this to report:
// -> A client has access to the frontend. It communicates with this part and this part alone.
// -> It is then these frontends which communicate with the servers (replica managers) behind the scenes
// -> They are responsible for coordinating all the

type Bidder struct {
	name        string
	isConnected bool
}

type Frontend struct {
	id             string
	RMPorts        []int32
	auctionBidders []proto.AuctionServiceClient
}

func main() {
	bidderName := os.Args[1]
	bidder := &Bidder{
		name:        bidderName,
		isConnected: false,
	}

	// * We create ...
	frontend := &Frontend{
		id:             bidderName,
		RMPorts:        []int32{5000, 5001, 5002},
		auctionBidders: []proto.AuctionServiceClient{},
	}

	// * Establish connection with a frontend node:
	status, err := frontend.ConnectToFrontend()
	// * Print out list of failed and successful connections
	if err != nil {
		log.Fatal(err)
	} else {
		log.Println(status)
	}

	// * Confirm connection...
	if len(frontend.auctionBidders) > 0 {
		bidder.isConnected = true
	}

	// * Run goroutine:
	go QueryBidder(bidder, frontend)

	select {}
}

func QueryBidder(bidder *Bidder, frontend *Frontend) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		scanVal := scanner.Text()
		if strings.HasPrefix(scanVal, "bid") {
			bidAmount, err := strconv.Atoi(strings.Split(scanVal, " ")[1])
			if err != nil {
				log.Println("Bid was not a number.")
				continue
			}
			bidder.Bid(uint32(bidAmount), frontend)

		} else if strings.HasPrefix(scanVal, "result") {
			bidder.GetAuctionResult(frontend)
		}
	}
}

func (FE *Frontend) ConnectToFrontend() (string, error) {
	for _, port := range FE.RMPorts {
		addr := "localhost:" + strconv.Itoa(int(port))
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Unable to connect to port: %d", port)
			continue
			// TODO: Add to an array of unsuccessful port connection...
		}
		log.Printf("Successfully connected to port: %d\n", port)
		FE.auctionBidders = append(FE.auctionBidders, proto.NewAuctionServiceClient(conn))
	}

	if len(FE.auctionBidders) == 0 {
		return "No replica managers available", fmt.Errorf("no replica managers connected")
	}

	return "Successfully connected to all desired frontend nodes.", nil
}

// * [BIDDER] Make a bid:
func (bidder *Bidder) Bid(bidAmount uint32, FE *Frontend) {
	FE.Bid(bidder.name, bidAmount)
	log.Printf("You bid %d of dollars\n", bidAmount)
}

// * [FRONTEND] Make a bid:
func (FE *Frontend) Bid(bidderName string, bidAmount uint32) {

	// * Iterate over all the auction bidders and confirm
	for _, auctionServiceClient := range FE.auctionBidders {
		res, err := auctionServiceClient.Bid(context.Background(), &proto.T_Bid{Bidder: bidderName, Amount: bidAmount})

		if err != nil {
			FE.auctionBidders = RemoveFromArr(FE.auctionBidders, auctionServiceClient)
			log.Fatalf("Unable to bid bid %d for bidder: %s - Error: %s", bidAmount, bidderName, err.Error())
		} else {
			switch res.BidStatus {
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

	res, err := FE.auctionBidders[0].GetFinalHighestBid(context.Background(), &proto.Empty{})
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
