package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/strangelove-ventures/interchaintest/v7/examples/ethereum/e2esuite"
	"github.com/stretchr/testify/suite"
)

type OutpostTestSuite struct {
	e2esuite.TestSuite
	// The private key of a test account
	key *ecdsa.PrivateKey
	// The private key of the faucet account of interchaintest
	deployer *ecdsa.PrivateKey
}

func (s *OutpostTestSuite) SetupSuite(ctx context.Context) {
	s.TestSuite.SetupSuite(ctx)

	eth, canined := s.ChainA, s.ChainB
	fmt.Println(eth)
	fmt.Println(canined)

	s.Require().True(s.Run("Set up environment", func() {
		err := os.Chdir("../..") // Change directories for what?
		s.Require().NoError(err)

		s.key, err = eth.CreateAndFundUser()
		s.Require().NoError(err)

		operatorKey, err := eth.CreateAndFundUser()
		fmt.Println(operatorKey)
		s.Require().NoError(err)

		s.deployer, err = eth.CreateAndFundUser()
		s.Require().NoError(err)
	}))

	s.Require().True(s.Run("Deploy ethereum contracts", func() {
		// seems the operator key is for supporting proofs
		// we're not running proofs atm

		// note: can't just pick a name--need actual name of contract. This is case sensitive

		/* NOTE:
		We ran the command:
		forge script --rpc-url http://127.0.0.1:52078 --broadcast --non-interactive
		-vvvv /Users/biphan/jackal/ict-evm/examples/ethereum/scripts/SimpleStorage.s.sol:SimpleStorage

		in our local terminal and it worked
		This means the 'ForgeScript' function is actually targeting our local file system,
		which means creating a mount bind between local scripts directory and the container was pointless?
		*/

		fmt.Println("****deployment complete****")
	}))
}

func TestWithOutpostTestSuite(t *testing.T) {
	suite.Run(t, new(OutpostTestSuite))
}

func (s *OutpostTestSuite) TestDummy() {
	ctx := context.Background()
	s.SetupSuite(ctx)

	canined := s.ChainB
	fmt.Println(canined)

	s.Require().True(s.Run("dummy", func() {
		fmt.Println("made it here")
		time.Sleep(10 * time.Hour)
	}))
}
