package main

import (
	"context"
	"fmt"
	"log"
	"os"

	bidderregistry "event_tracker/bidder_registry"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "event-tracker",
		Usage: "Track bidder registry events",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "address",
				Aliases:  []string{"a"},
				Usage:    "Ethereum address to check for deposits",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			address := c.String("address")
			return trackEvents(address)
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func trackEvents(addressStr string) error {
	client := initClient()

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get chain id: %v", err)
	}
	fmt.Println("Chain ID: ", chainID)

	contractAddress := common.HexToAddress("0xC973D09e51A20C9Ab0214c439e4B34Dbac52AD67")

	brf, err := bidderregistry.NewBidderregistryFilterer(contractAddress, client)
	if err != nil {
		return fmt.Errorf("failed to create Bidder Registry caller: %v", err)
	}

	address := common.HexToAddress(addressStr)
	fmt.Println("Monitoring address:", address.Hex())

	depositedWindows := make(map[string]bool)

	filterOpts := &bind.FilterOpts{
		End:     nil,
		Context: context.Background(),
	}

	events, err := brf.FilterBidderRegistered(filterOpts, []common.Address{address}, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to filter Bidder Added events: %v", err)
	}

	for events.Next() {
		event := events.Event
		windowNumber := event.WindowNumber.String()
		if _, ok := depositedWindows[windowNumber]; !ok {
			depositedWindows[windowNumber] = true
		}
	}

	withdrawalIter, err := brf.FilterBidderWithdrawal(filterOpts, []common.Address{address}, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to filter Withdrawal events: %v", err)
	}

	for withdrawalIter.Next() {
		withdrawal := withdrawalIter.Event
		windowNumber := withdrawal.Window.String()
		delete(depositedWindows, windowNumber)
	}

	fmt.Println("Deposited windows w/o withdrawals: ", len(depositedWindows))

	for windowNumber := range depositedWindows {
		fmt.Println("Window number: ", windowNumber)
	}

	return nil
}

func initClient() *ethclient.Client {
	client, err := ethclient.Dial("https://chainrpc.mev-commit.xyz")
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
	}
	return client
}
