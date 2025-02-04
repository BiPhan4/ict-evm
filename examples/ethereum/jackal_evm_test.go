package main

import (
	"context"
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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var EvmUserA string

func (s *OutpostTestSuite) TestJackalEVMBridge() {
	ctx := context.Background()
	s.SetupJackalEVMBridgeSuite(ctx)
	defer logFile.Close()

	// Fund jackal account

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
	addressAString := addressA.String()
	EvmUserA = addressAString
	addressAHex := addressA.Hex()

	fmt.Printf("addressAString: %s", addressAString)
	fmt.Printf("addressAHex: %s", addressAHex)

	msg := factorytypes.ExecuteMsg{
		CreateBindings: &factorytypes.ExecuteMsg_CreateBindings{UserEvmAddress: &EvmUserA},
	}
	// WARNING: possible that we made a bindings contract for the wrong address. Or the address was empty when we sent the below tx
	// and it failed silently.
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

	time.Sleep(30 * time.Second)

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
	returnedContractAddr, err := ethWrapper.ForgeCreate(privKeyA, "JackalBridge", pathOfOutpost, relays, priceFeed) // fails with "abi: attempting to unmarshal an empty string while arguments are expected", shows up a few seconds later
	if err != nil {
		log.Fatalf("Failed to deploy simple storage: %v", err)
	}

	ContractAddress = returnedContractAddr
	fmt.Printf("JackalBridge deployed at: %s\n", ContractAddress)

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

	// Call `postFile` on the deployed JackalBridge contract
	functionSig := "postFile(string,uint64,string)"
	args := []string{merkleHex, filesize, ""}

	txHash, err := ethWrapper.CastSend(ContractAddress, functionSig, args, rpcURL, privateKeyA, value)
	fmt.Printf("tx hash is: %s\n", txHash)
	if err != nil {
		log.Fatalf("Failed to call `postFile` on the contract: %v", err)
	}

	time.Sleep(30 * time.Second)

	// Call `buyStorage` on deployed JackalBridge contract
	functionSig = "buyStorage(string,uint64,uint64,string)"
	args = []string{"jkl12g4qwenvpzqeakavx5adqkw203s629tf6k8vdg", "30", "1073741824", "sample referral"} // 1 gb-month
	txHash, err = ethWrapper.CastSend(ContractAddress, functionSig, args, rpcURL, privateKeyA, value)
	if err != nil {
		log.Fatalf("Call `buyStorage` failed on contract: %v", err)
	}
	fmt.Printf("tx hash: %s\n", txHash)

	s.Require().True(s.Run("forge", func() {
		fmt.Println("made it to the end")
	}))
	time.Sleep(10 * time.Hour) // if this is active vscode thinks test fails
}
