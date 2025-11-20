// client/client.go
package main

import (
	proto "Auction/grpc"
	"context"
	"strconv"

	//"flag"
	"fmt"
	//"log"
	"os"
	//"strconv"
	//"time"
	"bufio"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// func usage() {
//     fmt.Println("Usage:")
//     fmt.Println("  client -server localhost:5000 bid <bidder> <amount>")
//     fmt.Println("  client -server localhost:5000 result")
// }

func connectToServer(address string) (proto.AuctionServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		fmt.Printf("Failed to connect to server: %v", err)
	}
	client := proto.NewAuctionServiceClient(conn)
	return client, conn, nil
}

func pingServer(stream proto.AuctionService_TrafficClient, username string) {
	ping := &proto.AuctionMessage{
		Username: username,
		Type:     "ping",
	}
	stream.Send(ping)
}

func main() {
	// ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	// defer cancel()

	client, conn, _ := connectToServer("localhost:6000")
	stream, _ := client.Traffic(context.Background())

	defer conn.Close()

	// Make a queue that we can later pop called replicas
	replicas := []string{}

	// Get username from user input
	fmt.Print("Enter your username: ")
	reader := bufio.NewReader(os.Stdin)
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	// Initial ping to get replicas
	pingServer(stream, username)

	highestBid := 0

	// Handle recieved messages
	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				if replicas != nil {
					// pop queue
					next_server := replicas[0]
					client, conn, _ = connectToServer(next_server)
					stream, _ = client.Traffic(context.Background())
				} else {
					fmt.Printf("No more servers")
				}
			}

			if in.Type == "replicas" {
				// Add replicas to queue
				replicas = in.Replicas
			}

			if in.Type == "bid" {
				user := strings.TrimSpace(in.Username)
				amount := int(in.Amount)
				fmt.Printf("%s has bid %d\n", user, amount)
				highestBid = amount
			}

			if in.Type == "result" {
				user := strings.TrimSpace(in.Username)
				amount := int(in.Amount)
				fmt.Printf("Auction closed! Winner is %s with a bid of %d\n", user, amount)
			}
		}
	}()

	// Handle send messages
	for {
		fmt.Print("Enter your bid (or 'exit' to quit): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "exit" {
			fmt.Println("Exiting...")
			break
		}

		bidAmount, err := strconv.Atoi(input)
		if err != nil {
			fmt.Println("Invalid bid amount. Please enter a valid number.")
			continue
		}

		if bidAmount <= highestBid {
			fmt.Printf("Your bid must be higher than the current highest bid of %d\n", highestBid)
			continue
		}

		bidMessage := &proto.AuctionMessage{
			Username: username,
			Type:     "bid",
			Amount:   uint32(bidAmount),
		}
		stream.Send(bidMessage)
	}
}
