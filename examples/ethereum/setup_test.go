package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/strangelove-ventures/interchaintest/v7"
	"github.com/strangelove-ventures/interchaintest/v7/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/chainconfig"
	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/e2esuite"
	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/eth"
	factorytypes "github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/types/bindingsfactory"
	logger "github.com/strangelove-ventures/interchaintest/v7/examples/logger"
	"github.com/strangelove-ventures/interchaintest/v7/testreporter"

	"go.uber.org/zap/zaptest"
)

var (
	canineRPCAddress string
	localConfigPath  string
	factoryAddress   string
	logFile          *os.File
)

func (s *OutpostTestSuite) SetupJackalEVMBridgeSuite(ctx context.Context) {
	// Start Anvil node
	anvilArgs := []string{"--port", "8545", "--block-time", "1", "--host", "0.0.0.0", "-vvvvv"}
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

	// Start canine-chain with 3 validators
	icChainSpecs := chainconfig.ChainSpecs

	s.TestSuite.Logger = zaptest.NewLogger(s.T())
	s.TestSuite.DockerClient, s.Network = interchaintest.DockerSetup(s.T())

	cf := interchaintest.NewBuiltinChainFactory(s.Logger, icChainSpecs)

	chains, err := cf.Chains(s.T().Name())
	s.Require().NoError(err)

	// canine-chain should be at index 0

	ic := interchaintest.NewInterchain()
	for _, chain := range chains {
		ic = ic.AddChain(chain)
	}

	s.TestSuite.ExecRep = testreporter.NewNopReporter().RelayerExecReporter(s.T())

	// TODO: Run this in a goroutine and wait for it to be ready
	s.Require().NoError(ic.Build(ctx, s.ExecRep, interchaintest.InterchainBuildOptions{
		TestName:         s.T().Name(),
		Client:           s.DockerClient,
		NetworkID:        s.Network,
		SkipPathCreation: true,
	}))

	canine := chains[0].(*cosmos.CosmosChain)
	canineRPC := canine.GetRPCAddress()
	canineRPCAddress = canineRPC
	log.Printf("canine-chain rpc is: %s", canineRPCAddress)
	caninedHostRPC := canine.GetHostRPCAddress()
	log.Printf("canine-chain host rpc is: %s", caninedHostRPC)
	caninedHostGRPC := canine.GetHostGRPCAddress()

	// NOTE: I think Mulberry should be able to listen to canine-chain using '127.0.0.1' now
	// TODO: change it back to local host then
	updatedCanineHostRPC := strings.Replace(caninedHostRPC, "127.0.0.1", "host.docker.internal", 1)
	log.Printf("updatedCanineHostRPC is: %s", updatedCanineHostRPC)

	// returned canine-chain rpc is: http://puppy-1-fn-0-TestWithOutpostTestSuite_TestJackalEVMBridge:26657
	// and canine-chain host rpc is: http://127.0.0.1:59026

	// Mulberry just has to ping it using , e.g. http://host.docker.internal:59026 -- recreate this with each run
	// So we should boot canine-chain before mulberry

	// WARNING: This number can't be too high or the faucet can't seem to have enough to fund accounts
	// Perfect number is between 10_000_000_000 and 1_000_000_000_000
	const userFunds = int64(1_000_000_000_000)
	// userFundsInt := math.NewInt(userFunds) formerly used 'cosmossdk.io/math int64 type'

	// Why did I have to do this?
	// I thought ic build process assinged the chain automatically?
	s.ChainB = canine

	// Danny's seed phrase was here before

	// Do we need a second Jackal User?

	// setup Mulberry, pull image
	var image string
	switch runtime.GOARCH {
	case "arm64":
		image = "biphan4/mulberry:0.0.10"
	case "amd64":
		image = "anthonyjackallabs/mulberry"
	default:
		log.Fatalf("unsupported architecture %s", runtime.GOARCH)
	}

	if err := e2esuite.PullMulberryImage(image); err != nil {
		log.Fatalf("Error pulling Docker image: %v", err)
	}

	// Get the absolute path of the local config file
	createdLocalConfigPath, err := filepath.Abs("e2esuite/mulberry_config.yaml")
	if err != nil {
		log.Fatalf("failed to resolve config path: %v", err)
	}
	localConfigPath = createdLocalConfigPath

	// Run the container, stream logs
	containerID, err := e2esuite.RunContainerWithConfig(image, "mulberry", localConfigPath)
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

	// Update the YAML file to connect with anvil
	rpcAddress := "http://127.0.0.1:8545"
	wsAddress := "ws://127.0.0.1:8545"
	if err := e2esuite.UpdateMulberryConfigRPC(localConfigPath, "Ethereum Sepolia", rpcAddress, wsAddress); err != nil {
		log.Fatalf("Failed to update mulberry config: %v", err)
	}

	if err := e2esuite.UpdateMulberryJackalRPC(localConfigPath, caninedHostRPC, caninedHostGRPC); err != nil {
		log.Fatalf("Failed to update canine-chain rpc address: %v", err)
	}

	log.Printf("Updated mulberry config with WS address: %s\n", wsAddress)

	// Start Mulberry
	// NOTE: get logs some other way, streaming the output of 'start' is blocking the rest of the code
	startCommand := []string{"sh", "-c", "export NO_COLOR=true; mulberry start >> /proc/1/fd/1 2>> /proc/1/fd/2"}
	if err := e2esuite.ExecCommandInContainer(containerID, startCommand); err != nil {
		log.Fatalf("Error starting mulberry in container: %v", err)
	}

	// TODO: remove this sleep eventually if it's not needed
	time.Sleep(5 * time.Second)
	// retrieve mulberry's jkl seed

	filePath := "/root/.mulberry/seed.json"

	contents, err := e2esuite.RetrieveFileFromContainer(containerID, filePath)
	if err != nil {
		log.Fatalf("Failed to retrieve file: %v", err)
	}

	fmt.Printf("Retrieved content length: %d\n", len(contents))

	fmt.Printf("Contents of %s:\n%s\n", filePath, contents)
	fmt.Printf("===============\n\n\n")
	fmt.Printf("%s\n", contents)

	// There seems to be a # attached to the last word of the seed
	// Trim trailing `#` and whitespace
	cleanedContents := strings.TrimRight(contents, "# \n")
	fmt.Printf("Cleaned Contents of %s:\n%s\n", filePath, cleanedContents)

	// Build a new clean string
	var words []string
	for _, word := range strings.Fields(cleanedContents) {
		cleanWord := removeNonPrintable(word)
		words = append(words, cleanWord)
	}
	reconstructedString := strings.Join(words, " ")

	fmt.Println("Reconstructed String:")
	fmt.Println(reconstructedString)
	fmt.Printf("String Length: %d\n", len(reconstructedString))
	fmt.Printf("Raw Bytes: %q\n", []byte(cleanedContents))

	// Remove the pesky symbol at index 0 if it exists
	if len(reconstructedString) > 0 && []rune(reconstructedString)[0] == '\uFFFD' {
		// Remove the pesky symbol at index 0
		reconstructedString = string([]rune(reconstructedString)[1:])
	}

	// Confirm the pesky symbol is gone
	verifyString(reconstructedString)

	// Proceed with the cleaned string
	mulberrySeed := reconstructedString

	userB, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "jkl", mulberrySeed, userFunds, s.ChainB)
	s.Require().NoError(err)

	s.UserB = userB // the jackal user
	fmt.Printf("Mulberry's jkl account: %s\n", userB.FormattedAddress())

	// This is the user in our cosmwasm_signer, so we ensure they have funds
	s.FundAddressChainB(ctx, s.UserB.FormattedAddress())

	// Store code of bindings factory
	FactoryCodeId, err := s.ChainB.StoreContract(ctx, s.UserB.KeyName(), "../wasm_artifacts/bindings_factory.wasm")
	s.Require().NoError(err)
	fmt.Println(FactoryCodeId)

	// Store code of filetree bindings
	BindingsCodeId, error := s.ChainB.StoreContract(ctx, s.UserB.KeyName(), "../wasm_artifacts/canine_bindings.wasm")
	s.Require().NoError(error)
	fmt.Println(BindingsCodeId)

	// codeId is string and needs to be converted to uint64
	BindingsCodeIdAsInt, err := strconv.ParseInt(BindingsCodeId, 10, 64)
	s.Require().NoError(err)

	// NOTE: We should have imported factorytypes from jackal-evm but that repo is too big and messy
	// which causes the 'module source tree too large' error when running: go get github.com/JackalLabs/jackal-evm@e75940283544bade2b37bf1e0523563289184aca

	// TODO: import factorytypes from 'jackal-evm' when jackal-evm is cleaned up
	// Instantiate the factory, giving it the codeId of the filetree bindings contract
	instantiateMsg := factorytypes.InstantiateMsg{BindingsCodeId: int(BindingsCodeIdAsInt)}

	contractAddr, _ := s.ChainB.InstantiateContract(ctx, s.UserB.KeyName(), FactoryCodeId, toString(instantiateMsg), false, "--gas", "500000", "--admin", s.UserB.KeyName())
	// s.Require().NoError(err)
	fmt.Printf("factory contract address: %s\n", contractAddr)
	// TODO: give Mulberry factory contract address

	// NOTE: Looks like Mulberry is calling the factory
	// TODO: double check that mulberry is in fact calling the factory, and not the bindings directly

	// NOTE: The contractAddr can't be retrived at this time because of sdk tx parsing errors we noted in 'jackal-evm' repo
	// We can fix that later but for now, we'll just hard code the consistent factory contract address
	factoryContractAddress := "jkl14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9scsc9nr"
	factoryAddress = factoryContractAddress

	fmt.Println(factoryContractAddress)
	// TODO: we can put the bindings contract address here?

	contractState, stateErr := e2esuite.GetState(ctx, s.ChainB, factoryContractAddress)
	s.Require().NoError(stateErr)
	logger.LogInfo(contractState)

	// Update the YAML file to connect with canine-chain
	// WARNING: if Mulberry can't broadcast the CosmWasm tx, this is the first point of inspection
	if err := e2esuite.UpdateMulberryJackalConfig(localConfigPath, caninedHostRPC, factoryContractAddress); err != nil { // Mulberry should be able to see local host
		log.Fatalf("Failed to update mulberry's jackal config: %v", err)
	}

	// Fund the factory so it can fund the bindings
	s.FundAddressChainB(ctx, factoryContractAddress)

	fmt.Printf("evm user A: %s", EvmUserA)
}

// Helper function to remove non-printable characters
func removeNonPrintable(input string) string {
	var result []rune
	for _, r := range input {
		if unicode.IsPrint(r) {
			result = append(result, r)
		}
	}
	return string(result)
}

func verifyString(content string) {
	fmt.Printf("String Length: %d\n", len(content))

	for i, r := range content {
		if r == '�' { // Check for the replacement character
			fmt.Printf("Found pesky symbol at index %d\n", i)
			return
		}
	}

	fmt.Println("No pesky symbol found in the string.")
}

// log address of bindings contract
// create bindings factory contract

// toString converts the message to a string using json
func toString(msg any) string {
	bz, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}

	return string(bz)
}
