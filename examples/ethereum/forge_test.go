package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/e2esuite"
	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/eth"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	SimpleStorageAddressKey = "SimpleStorageAddress"
)

var (
	ContractAddress string
	containerID     string
)

func (s *OutpostTestSuite) SetupForgeSuite(ctx context.Context) {
	// Start Anvil node
	anvilArgs := []string{"--port", "8545", "--block-time", "1", "--host", "0.0.0.0"}
	// easiest way to install anvil is foundryup --install stable
	// you can modify the code to use docker container with --network host
	output, err := eth.ExecuteCommand("anvil", anvilArgs)
	if err != nil {
		fmt.Printf("Error starting Anvil: %s\n", err)
		return
	}
	fmt.Printf("Anvil Output: %s\n", output)

	// Poll for Anvil readiness
	fmt.Println("Waiting for Anvil to become ready...")
	rpcURL := "http://127.0.0.1:8545"
	if err := eth.WaitForRPC(rpcURL, 10*time.Second); err != nil {
		fmt.Printf("Error: Anvil did not become ready in time: %s\n", err)
		return
	}
	fmt.Println("Anvil is ready at", rpcURL)

	// setup Mulberry, pull image
	var image string
	switch runtime.GOARCH {
	case "arm64":
		image = "biphan4/mulberry:0.0.9"
	case "amd64":
		image = "anthonyjackallabs/mulberry"
	default:
		log.Fatalf("unsupported architecture %s", runtime.GOARCH)
	}

	if err := e2esuite.PullMulberryImage(image); err != nil {
		log.Fatalf("Error pulling Docker image: %v", err)
	}

	// Get the absolute path of the local config file
	localConfigPath, err := filepath.Abs("e2esuite/mulberry_config.yaml")
	if err != nil {
		log.Fatalf("failed to resolve config path: %v", err)
	}

	// Run the container, stream logs
	containerID, err = e2esuite.RunContainerWithConfig(image, "mulberry", localConfigPath)
	if err != nil {
		log.Fatalf("Error running container: %v", err)
	}

	logFile, err = os.Create("mulberry_logs.txt")
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}

	go func() {
		err := e2esuite.StreamContainerLogsToFile(containerID, logFile)
		if err != nil {
			log.Printf("Failed to stream Mulberry logs to file: %v", err)
		}
	}()

	// Give mulberry a wallet
	addressCommand := []string{"sh", "-c", "mulberry wallet address >> /proc/1/fd/1 2>> /proc/1/fd/2"}
	if err := e2esuite.ExecCommandInContainer(containerID, addressCommand); err != nil {
		log.Fatalf("Error creating wallet address in container: %v", err)
	}

	// Update the YAML file
	rpcAddress := "http://127.0.0.1:8545"
	wsAddress := "ws://127.0.0.1:8545"
	if err := e2esuite.UpdateMulberryConfigRPC(localConfigPath, "Ethereum Sepolia", rpcAddress, wsAddress); err != nil {
		log.Fatalf("Failed to update mulberry config: %v", err)
	}

	log.Printf("Updated mulberry config with WS address: %s\n", wsAddress)

	// Start Mulberry
	// NOTE: get logs some other way, streaming the output of 'start' is blocking the rest of the code
	startCommand := []string{"sh", "-c", "export NO_COLOR=true; mulberry start >> /proc/1/fd/1 2>> /proc/1/fd/2"}
	if err := e2esuite.ExecCommandInContainer(containerID, startCommand); err != nil {
		log.Fatalf("Error starting mulberry in container: %v", err)
	}
}

func (s *OutpostTestSuite) TestForge() {
	// intercept SIGTERM (Ctrl + C)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		cleanForgeSuite()
	}()

	ctx := context.Background()
	s.SetupForgeSuite(ctx)

	// Connect to Anvil RPC
	rpcURL := "http://127.0.0.1:8545"
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
	}
	defer client.Close()

	// Let's use account (9) as the faucet
	faucetPrivateKeyHex := "0x2a871d0798f97d79848a013d4936a73bf4cc922c825d33c1cf7073dff6d409c6"
	faucetPrivateKey, err := crypto.HexToECDSA(faucetPrivateKeyHex[2:]) // Remove "0x" prefix
	if err != nil {
		log.Fatalf("Failed to parse faucet private key: %v", err)
	}

	// Create the Ethereum object
	ethWrapper, err := eth.NewEthereum(ctx, rpcURL, faucetPrivateKey)
	if err != nil {
		log.Fatalf("Failed to initialize Ethereum object: %v", err)
	}

	log.Printf("Ethereum object initialized: %+v", ethWrapper)

	// Define accounts and their private keys
	privateKeyA := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	addressB := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	fmt.Println(addressB)

	// Convert accountA's private key string to *ecdsa.PrivateKey
	privKeyA, err := crypto.HexToECDSA(privateKeyA[2:]) // Remove "0x" prefix
	if err != nil {
		log.Fatalf("Failed to parse private key: %v", err)
	}

	// Get the public address of Account A
	addressA := crypto.PubkeyToAddress(privKeyA.PublicKey)
	fmt.Println(addressA)

	// Check Account A's nonce
	nonce, err := client.PendingNonceAt(context.Background(), addressA)
	if err != nil {
		log.Fatalf("Failed to get nonce for Account A: %v", err)
	}
	fmt.Printf("Account A's nonce is %d\n", nonce)

	// Bump the nounce
	nonce = nonce + 1

	// Get chain ID from the client
	chainID, err := client.NetworkID(context.Background())
	fmt.Printf("Chain ID is: %d\n", chainID)
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}

	// Prepare the transaction
	amount := new(big.Int).Mul(big.NewInt(35), big.NewInt(1e18)) // 35 ETH in wei
	gasLimit := uint64(21000)
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		log.Fatalf("Failed to get gas price: %v", err)
	}

	tx := types.NewTransaction(nonce, addressB, amount, gasLimit, gasPrice, nil)

	// Sign the transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privKeyA)
	if err != nil {
		log.Fatalf("Failed to sign transaction: %v", err)
	}

	// Send the transaction
	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}
	fmt.Printf("Transaction sent: %s\n", signedTx.Hash().Hex())

	// Query Account B's balance to ensure it received the 35 ETH
	balanceB, err := client.BalanceAt(context.Background(), addressB, nil)
	if err != nil {
		log.Fatalf("Failed to query balance for Account B: %v", err)
	}

	fmt.Printf("Account B balance: %s ETH\n", new(big.Float).Quo(new(big.Float).SetInt(balanceB), big.NewFloat(1e18)).String())

	// pathOfScripts := filepath.Join(dir, "scripts/SimpleStorage.s.sol")
	dir, _ := os.Getwd() // note: returns the root of this repository: ict-evm/
	pathOfOutpost := filepath.Join(dir, "/../../forge/src/JackalV1.sol")

	relays := []string{
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
	}
	priceFeed := "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"

	// WARNING: remember to add the price feed back into the contract
	// note: how on earth is the command still consuming 'priceFeed' when I removed it from the contract's
	// constructor?

	// Deploy the JackalBridge contract
	// The deployer is the owner of the contract, and who is allowed to relay the event--I think?
	returnedContractAddr, err := ethWrapper.ForgeCreate(privKeyA, "JackalBridge", pathOfOutpost, relays, priceFeed)
	if err != nil {
		log.Fatalf("Failed to deploy simple storage: %v", err)
	}

	ContractAddress = returnedContractAddr
	fmt.Printf("JackalBridge deployed at: %s\n", ContractAddress)

	// Note: I wonder if this is Mulberry's issue: trying to use an RPC client
	// To establish the WS connection?
	// Connect to Anvil WS
	wsURL := "ws://127.0.0.1:8545"
	wsClient, err := ethclient.Dial(wsURL)
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum ws client: %v", err)
	}
	defer wsClient.Close()

	go eth.ListenToLogs(wsClient, common.HexToAddress(ContractAddress))

	// Define the parameters for the `postFile` function
	merkle := "placeholder-merkle-root"
	filesize := "1048576" // 1 MB in bytes (as string)

	// Given value
	value := big.NewInt(5000000000000)

	// Call `postFile` on the deployed JackalBridge contract
	functionSig := "postFile(string,uint64)"
	args := []string{merkle, filesize}

	txHash, err := ethWrapper.CastSend(ContractAddress, functionSig, args, rpcURL, privateKeyA, value)
	fmt.Printf("tx hash is: %s\n", txHash)
	if err != nil {
		log.Fatalf("Failed to call `postFile` on the contract: %v", err)
	}

	s.Require().True(s.Run("forge", func() {
		fmt.Println("made it to the end")
	}))

	time.Sleep(10 * time.Hour) // if this is active vscode thinks test fails
	logFile.Close()
}

func cleanForgeSuite() {
	eth.ExecuteCommand("killall", []string{"anvil"})
	e2esuite.StopContainer(containerID)
	time.Sleep(10 * time.Second)
	os.Exit(1)
}
