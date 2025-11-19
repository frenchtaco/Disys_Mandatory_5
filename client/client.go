// client/client.go
package main

import (
	proto "Auction/grpc"
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    "strconv"
    "time"

    
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func usage() {
    fmt.Println("Usage:")
    fmt.Println("  client -server localhost:5000 bid <bidder> <amount>")
    fmt.Println("  client -server localhost:5000 result")
}

func main() {
    serverAddr := flag.String("server", "localhost:5000", "auction server address")
    timeoutSec := flag.Int("timeout", 5, "RPC timeout in seconds")
    flag.Parse()

    args := flag.Args()
    if len(args) < 1 {
        usage()
        os.Exit(1)
    }

    cmd := args[0]

    ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
    defer cancel()

    conn, err := grpc.DialContext(
        ctx,
        *serverAddr,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithBlock(),
    )
    if err != nil {
        log.Fatalf("failed to connect to %s: %v", *serverAddr, err)
    }
    defer conn.Close()

    client := proto.NewAuctionServiceClient(conn)

    switch cmd {
    case "bid":
        if len(args) != 3 {
            fmt.Println("bid requires <bidder> <amount>")
            usage()
            os.Exit(1)
        }
        bidder := args[1]
        amtInt, err := strconv.Atoi(args[2])
        if err != nil || amtInt <= 0 {
            log.Fatalf("invalid amount: %v", args[2])
        }

        resp, err := client.Bid(ctx, &proto.BidRequest{
            Bidder: bidder,
            Amount: uint32(amtInt),
        })
        if err != nil {
            log.Fatalf("Bid RPC error: %v", err)
        }

        fmt.Printf("Bid outcome: %v\n", resp.Outcome)
        fmt.Printf("Message: %s\n", resp.Message)

    case "result":
        resp, err := client.Result(ctx, &proto.ResultRequest{})
        if err != nil {
            log.Fatalf("Result RPC error: %v", err)
        }

        if resp.Closed {
            fmt.Println("Auction status: CLOSED")
            if resp.Winner == "" {
                fmt.Println("No winner.")
            } else {
                fmt.Printf("Winner: %s\n", resp.Winner)
                fmt.Printf("Winning amount: %d\n", resp.WinningAmount)
            }
        } else {
            fmt.Println("Auction status: OPEN")
            fmt.Printf("Current highest: %d\n", resp.CurrentHighest)
        }

    default:
        fmt.Printf("unknown command: %s\n", cmd)
        usage()
        os.Exit(1)
    }
}
