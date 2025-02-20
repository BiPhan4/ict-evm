package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/e2esuite"
	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/eth"
	factorytypes "github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/types/bindingsfactory"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func (s *OutpostTestSuite) TestStress() {
	ctx := context.Background()
	s.SetupMulberrySuite(ctx, "stress_log.txt")
	defer logFile.Close()

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

	privateKeyA := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	// Convert accountA's private key string to *ecdsa.PrivateKey
	privKeyA, err := crypto.HexToECDSA(privateKeyA[2:]) // Remove "0x" prefix
	if err != nil {
		log.Fatalf("Failed to parse private key: %v", err)
	}

	// Get the public address of Account A
	addressA := crypto.PubkeyToAddress(privKeyA.PublicKey)
	addressAString := addressA.String()
	EvmUserA = addressAString
	addressAHex := addressA.Hex()

	fmt.Printf("addressAString: %s", addressAString)
	fmt.Printf("addressAHex: %s", addressAHex)

	msg := factorytypes.ExecuteMsg{
		CreateBindings: &factorytypes.ExecuteMsg_CreateBindings{UserEvmAddress: &EvmUserA},
	}
	res, _ := s.ChainB.ExecuteContract(ctx, s.UserB.KeyName(), factoryAddress, msg.ToString(), "--gas", "500000")
	// NOTE: cannot parse res because of cosmos-sdk issue noted before, so we will get an error
	// fortunately, we went into the docker container to confirm that the post key msg does get saved into canine-chain
	fmt.Println(res)

	// Let's have the factory give evmUserA 200jkl
	fundingAmount := int64(200_000_000)

	factoryFundingExecuteMsg := factorytypes.ExecuteMsg{
		FundBindings: &factorytypes.ExecuteMsg_FundBindings{
			EvmAddress: &EvmUserA,
			Amount:     &fundingAmount,
		},
	}

	fundingRes, _ := s.ChainB.ExecuteContract(ctx, s.UserB.KeyName(), factoryAddress, factoryFundingExecuteMsg.ToString(), "--gas", "500000")
	fmt.Println(fundingRes)

	// Check Account A's nonce
	nonce, err := client.PendingNonceAt(context.Background(), addressA)
	if err != nil {
		log.Fatalf("Failed to get nonce for Account A: %v", err)
	}
	fmt.Printf("Account A's nonce is %d\n", nonce)

	// Get chain ID from the client
	chainID, err := client.NetworkID(context.Background())
	fmt.Printf("Chain ID is: %d\n", chainID)
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}

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
	returnedContractAddr, err := ethWrapper.ForgeCreate(privKeyA, "JackalBridge", pathOfOutpost, relays, priceFeed) // fails with "abi: attempting to unmarshal an empty string while arguments are expected", shows up a few seconds later
	if err != nil {
		log.Fatalf("Failed to deploy simple storage: %v", err)
	}

	ContractAddress = returnedContractAddr
	fmt.Printf("JackalBridge deployed at: %s\n", ContractAddress)
	testJKLAddress := "jkl12g4qwenvpzqeakavx5adqkw203s629tf6k8vdg"

	// NOTE: The name of the network shouldn't matter when trying to establish a connection
	// WARNING: double check finality. I think it's 2 but double check
	if err := e2esuite.UpdateMulberryConfigEVM(localConfigPath, "Ethereum Sepolia", ContractAddress, int(chainID.Int64())); err != nil {
		log.Fatalf("Failed to update mulberry config: %v", err)
	}

	// Note: I wonder if this is Mulberry's issue: trying to use an RPC client
	// To establish the WS connection?
	// Connect to Anvil WS
	wsURL := "ws://127.0.0.1:8545"
	wsClient, err := ethclient.Dial(wsURL)
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum ws client: %v", err)
	}
	defer client.Close()

	go eth.ListenToLogs(wsClient, common.HexToAddress(ContractAddress))

	// Define the parameters for the `postFile` function
	merkleBytes := []byte{0x01, 0x02, 0x03, 0x04}

	// Encode to hexadecimal
	merkleHex := hex.EncodeToString(merkleBytes)

	fmt.Println("Merkle Hex:", merkleHex)

	filesize := "1048576" // 1 MB in bytes (as string) // WARNING: possible invalid file size

	// Given value
	value := big.NewInt(5000000000000)
	zero := big.NewInt(0)
	var txHash string

	// the below calls test evm <-> mulberry <-> cosmwasm <-> canine
	for i := 0; i < 100; i++ {
		txHash, err = ethWrapper.CastSend(ContractAddress, "postFile(string,uint64,string,uint64)", []string{merkleHex, filesize, "", "30"}, rpcURL, privateKeyA, value)
		if log.Println(txHash); err != nil {
			log.Fatalf("Call `postFile` failed on contract: %v", err)
		}
	}
	log.Println("100 `postFile` calls complete")

	txHash, err = ethWrapper.CastSend(ContractAddress, "buyStorage(string,uint64,uint64,string)", []string{testJKLAddress, "30", "1073741824", "sample referral"}, rpcURL, privateKeyA, value)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `buyStorage` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "deleteFile(string,uint64)", []string{merkleHex, "1"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `deleteFile` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "requestReportForm(string,string,string,uint64)", []string{"prover", merkleHex, testJKLAddress, "1"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `requestReportForm` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "postKey(string)", []string{"test key"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `postKey` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "provisionFileTree(string,string,string)", []string{"{}", "{}", "tracking123"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `provisionFileTree` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "postFileTree(string,string,string,string,string,string,string)", []string{"account", "parent hash", "child hash", "contents", "{}", "{}", "tracking123"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `postFileTree` failed on contract: %v", err) // fails for parent does not exist
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "deleteFileTree(string,string)", []string{"test/path", "account"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `deleteFileTree` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "addViewers(string,string,string,string)", []string{"viewer id", "viewer key", "for address", "file owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `addViewers` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "removeViewers(string,string,string)", []string{"viewer id", "for address", "file owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `removeViewers` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "resetViewers(string,string)", []string{"for address", "file owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `resetViewers` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "changeOwner(string,string,string)", []string{"for address", "old owner", "new owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `changeOwner` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "addEditors(string,string,string,string)", []string{"editor id", "editor key", "for address", "file owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `addEditors` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "removeEditors(string,string,string)", []string{"editor id", "for address", "file owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `removeEditors` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "resetEditors(string,string)", []string{"for address", "file owner"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `resetEditors` failed on contract: %v", err) // fails for file not found
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "createNotification(string,string,string)", []string{testJKLAddress, `{"key": "value"}`, base64.StdEncoding.EncodeToString([]byte("encrypted contents"))}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `createNotification` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "deleteNotification(string,uint64)", []string{testJKLAddress, "60"}, rpcURL, privateKeyA, zero)
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `deleteNotification` failed on contract: %v", err)
	}

	txHash, err = ethWrapper.CastSend(ContractAddress, "blockSenders(string[])", []string{`["` + testJKLAddress + `"]`}, rpcURL, privateKeyA, zero) // can also block registered .jkl names
	if log.Println(txHash); err != nil {
		log.Fatalf("Call `blockSenders` failed on contract: %v", err)
	}

	time.Sleep(5 * time.Second)
	s.Require().True(s.Run("forge", func() {
		fmt.Println("made it to the end")
	}))
	eth.ExecuteCommand("killall", []string{"anvil"})
	e2esuite.StopContainerByImage(image)
}
