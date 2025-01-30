package e2esuite

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/strangelove-ventures/interchaintest/v7"
	"github.com/strangelove-ventures/interchaintest/v7/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v7/ibc"
	"github.com/strangelove-ventures/interchaintest/v7/testreporter"

	chainconfig "github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/chainconfig"
	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/eth"
	logger "github.com/strangelove-ventures/interchaintest/v7/examples/logger"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ictethereum "github.com/strangelove-ventures/interchaintest/v7/chain/ethereum"
)

// Is this a new one or the one that already exists in eigenlayer-deployed-anvil-state.json
const anvilFaucetPrivateKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

type TestSuite struct {
	suite.Suite

	ChainA         eth.Ethereum
	EthTestnetType string
	ChainB         *cosmos.CosmosChain
	UserB          ibc.Wallet // At some point we will introduce the jackal user
	DockerClient   *dockerclient.Client
	Network        string
	Logger         *zap.Logger
	ExecRep        *testreporter.RelayerExecReporter

	// Don't need light clients for now. Only concerned about deploying outpost and
	// emitting events
}

// SetupSuite sets up the chains, mulberry relayer, user accounts, clients, and connections
func (s *TestSuite) SetupSuite(ctx context.Context) {
	logger.InitLogger()

	icChainSpecs := chainconfig.ChainSpecs
	logger.LogInfo(icChainSpecs)

	// At this step, the ibc team use a case statement to decide whether to boot up a POW or POS Eth chain.
	// We might need to do this in the future.

	s.Logger = zaptest.NewLogger(s.T())
	s.DockerClient, s.Network = interchaintest.DockerSetup(s.T())

	cf := interchaintest.NewBuiltinChainFactory(s.Logger, icChainSpecs)

	chains, err := cf.Chains(s.T().Name())
	s.Require().NoError(err)

	// canine-chain should be at index 1
	// s.ChainB = chains[1].(*cosmos.CosmosChain) // WARNING NOTE: Disabling for now, please turn back on

	ic := interchaintest.NewInterchain()
	for _, chain := range chains {
		ic = ic.AddChain(chain)
	}

	s.ExecRep = testreporter.NewNopReporter().RelayerExecReporter(s.T())

	// Disabling both chains for now
	// TODO: Run this in a goroutine and wait for it to be ready
	s.Require().NoError(ic.Build(ctx, s.ExecRep, interchaintest.InterchainBuildOptions{
		TestName:         s.T().Name(),
		Client:           s.DockerClient,
		NetworkID:        s.Network,
		SkipPathCreation: true,
	}))
	// fails on x86 because we use biphan4/foundry docker image
	// based on https://github.com/foundry-rs/foundry/blob/master/Dockerfile#L13

	// NOTE: We can map all query request types to their gRPC method paths for cosmos chains?
	// Easier/faster than making function(s) for jackal queries?

	anvil := chains[0].(*ictethereum.EthereumChain)

	faucet, err := crypto.ToECDSA(ethcommon.FromHex(anvilFaucetPrivateKey))
	s.Require().NoError(err)

	s.ChainA, err = eth.NewEthereum(ctx, anvil.GetHostRPCAddress(), faucet)
	s.Require().NoError(err)

	// Set up Mulberry

	// using local image for now
	image := "biphan4/mulberry:0.0.9"
	if err := PullMulberryImage(image); err != nil {
		log.Fatalf("Error pulling Docker image: %v", err)
	}

	containerName := "mulberry_test_container"

	// Get the absolute path of the local config file
	localConfigPath, err := filepath.Abs("e2esuite/mulberry_config.yaml")
	if err != nil {
		log.Fatalf("failed to resolve config path: %v", err)
	}

	// Run the container
	containerID, err := RunContainerWithConfig(image, containerName, localConfigPath)
	if err != nil {
		log.Fatalf("Error running container: %v", err)
	}

	log.Printf("Container is running with ID: %s\n", containerID)

	logFile, err := os.Create("mulberry_logs.txt")
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	go func() {
		err := StreamContainerLogsToFile(containerID, logFile)
		if err != nil {
			log.Printf("Failed to stream Mulberry logs to file: %v", err)
		}
	}()

	// Execute a command inside the container
	addressCommand := []string{"sh", "-c", "mulberry wallet address >> /proc/1/fd/1 2>> /proc/1/fd/2"}
	if err := ExecCommandInContainer(containerID, addressCommand); err != nil {
		log.Fatalf("Error creating wallet address in container: %v", err)
	}

	// NOTE: I'm paranoid and not 100% convinced these commands are executing inside the container, once the contract actually start emitting events
	// We will see whether the relayer can pick it up

	// Need an elegant way to modify mulberry's config to point to the anvil and canine-chain end points after they're spun up
	// Perhaps that's the next task
	// Before deploying the contract

	// logger.LogInfo("host rpc address: %s\n", anvil.GetHostRPCAddress())

	// Update the YAML file
	rpcAddress := "http://127.0.0.1:8545"
	wsAddress := "ws://127.0.0.1:8545"
	if err := UpdateMulberryConfigRPC(localConfigPath, "Ethereum Sepolia", rpcAddress, wsAddress); err != nil {
		log.Fatalf("Failed to update mulberry config: %v", err)
	}

	log.Printf("Updated mulberry config with RPC address: %s\n", rpcAddress)

	// TODO: fund jkl users
	// TODO: make a map of proposal IDs?

	// Start Mulberry
	startCommand := []string{"sh", "-c", "export NO_COLOR=true; mulberry start >> /proc/1/fd/1 2>> /proc/1/fd/2"}
	if err := ExecCommandInContainer(containerID, startCommand); err != nil {
		log.Fatalf("Error starting mulberry in container: %v", err)
	}

	// NOTE: it connected to RPC fine but looks like local anvil web socket is not exposed
	time.Sleep(10 * time.Hour)
}
