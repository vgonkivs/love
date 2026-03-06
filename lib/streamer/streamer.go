package streamer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	popsigner "github.com/Bidon15/popsigner/sdk-go"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	_ "github.com/celestiaorg/celestia-app/v6/app/params" // bech32 prefix init
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v3/share"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Streamer posts blobs to Celestia blockchain via POPSigner and gRPC
type Streamer struct {
	cfg       *Config
	conn      *grpc.ClientConn
	signer    *user.Signer
	txClient  cosmostx.ServiceClient
	keyName   string
	namespace share.Namespace
	encCfg    encoding.Config
}

// NewStreamer creates a new streamer with a random namespace
func NewStreamer(cfg *Config) (*Streamer, error) {
	// Generate random namespace bytes (10 bytes for user-defined namespace)
	nsBytes := make([]byte, 10)
	_, err := rand.Read(nsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate namespace: %w", err)
	}

	// Create namespace with version 0 prefix
	namespace, err := share.NewV0Namespace(nsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	return &Streamer{
		cfg:       cfg,
		namespace: namespace,
		encCfg:    encoding.MakeConfig(),
	}, nil
}

// Connect establishes connection to consensus node via gRPC and sets up POPSigner
func (s *Streamer) Connect(ctx context.Context) error {
	// Create POPSigner keyring
	kr, err := popsigner.NewCelestiaKeyring(s.cfg.PopSignerAPIKey, s.cfg.PopSignerKeyID)
	if err != nil {
		return fmt.Errorf("failed to create POPSigner keyring: %w", err)
	}

	// Connect to consensus node via gRPC
	conn, err := grpc.NewClient(
		s.cfg.GRPCAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to consensus node: %w", err)
	}
	s.conn = conn
	s.txClient = cosmostx.NewServiceClient(conn)

	// Get key record from POPSigner keyring
	records, err := kr.List()
	if err != nil {
		return fmt.Errorf("failed to list keys from POPSigner: %w", err)
	}
	if len(records) == 0 {
		return fmt.Errorf("no keys found in POPSigner keyring")
	}

	record := records[0]
	s.keyName = record.Name

	addr, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get address from key record: %w", err)
	}

	// Query account info from consensus node
	accNum, seqNum, err := user.QueryAccount(ctx, conn, s.encCfg.InterfaceRegistry, addr)
	if err != nil {
		return fmt.Errorf("failed to query account: %w", err)
	}

	// Create signer
	account := user.NewAccount(s.keyName, accNum, seqNum)
	signer, err := user.NewSigner(kr, s.encCfg.TxConfig, s.cfg.ChainID, account)
	if err != nil {
		return fmt.Errorf("failed to create signer: %w", err)
	}
	s.signer = signer

	log.Printf("Streamer: connected to consensus node at %s (address: %s, account: %d, sequence: %d)",
		s.cfg.GRPCAddress, addr.String(), accNum, seqNum)
	return nil
}

// Close closes the gRPC connection
func (s *Streamer) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// NamespaceHex returns the namespace as hex string
func (s *Streamer) NamespaceHex() string {
	return hex.EncodeToString(s.namespace.Bytes())
}

// Run receives blobs from input channel and submits them to Celestia
func (s *Streamer) Run(ctx context.Context, input <-chan []byte) error {
	if s.signer == nil {
		return fmt.Errorf("streamer not connected, call Connect() first")
	}

	log.Printf("Streamer: running (namespace: %s)", s.NamespaceHex())

	var blobCount uint64
	for {
		select {
		case <-ctx.Done():
			log.Printf("Streamer: stopping (submitted %d blobs)", blobCount)
			return nil
		case data, ok := <-input:
			if !ok {
				log.Printf("Streamer: input closed (submitted %d blobs)", blobCount)
				return nil
			}

			start := time.Now()
			err := s.submitBlob(ctx, data)
			if err != nil {
				log.Printf("Streamer: failed to submit blob: %v", err)
				continue
			}
			elapsed := time.Since(start)

			blobCount++
			log.Printf("Streamer: submitted blob %d (%d bytes) in %.3fs", blobCount, len(data), elapsed.Seconds())
		}
	}
}

// submitBlob creates a blob, signs a MsgPayForBlobs tx, and broadcasts it
func (s *Streamer) submitBlob(ctx context.Context, data []byte) error {
	// Create blob
	b, err := share.NewBlob(s.namespace, data, share.ShareVersionZero, nil)
	if err != nil {
		return fmt.Errorf("failed to create blob: %w", err)
	}

	// Estimate gas using a temporary MsgPayForBlobs with blob sizes
	gasEstMsg := &blobtypes.MsgPayForBlobs{
		BlobSizes: []uint32{uint32(len(data))},
	}
	gasLimit := blobtypes.DefaultEstimateGas(gasEstMsg)
	gasPrice := s.cfg.GasPrice
	if gasPrice == 0 {
		gasPrice = appconsts.DefaultMinGasPrice
	}

	// Build, sign, and encode the BlobTx
	blobTxBytes, _, err := s.signer.CreatePayForBlobs(
		s.keyName,
		[]*share.Blob{b},
		user.SetGasLimitAndGasPrice(gasLimit, gasPrice),
	)
	if err != nil {
		return fmt.Errorf("failed to create pay for blobs tx: %w", err)
	}

	// Broadcast the signed transaction
	resp, err := s.txClient.BroadcastTx(ctx, &cosmostx.BroadcastTxRequest{
		TxBytes: blobTxBytes,
		Mode:    cosmostx.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return fmt.Errorf("failed to broadcast tx: %w", err)
	}

	// Check for sequence mismatch (code 32) and retry once
	if resp.TxResponse != nil && resp.TxResponse.Code == 32 {
		log.Printf("Streamer: sequence mismatch, re-querying account...")
		if retryErr := s.refreshSequence(ctx); retryErr != nil {
			return fmt.Errorf("failed to refresh sequence: %w", retryErr)
		}

		// Rebuild and rebroadcast
		blobTxBytes, _, err = s.signer.CreatePayForBlobs(
			s.keyName,
			[]*share.Blob{b},
			user.SetGasLimitAndGasPrice(gasLimit, gasPrice),
		)
		if err != nil {
			return fmt.Errorf("failed to create pay for blobs tx (retry): %w", err)
		}

		resp, err = s.txClient.BroadcastTx(ctx, &cosmostx.BroadcastTxRequest{
			TxBytes: blobTxBytes,
			Mode:    cosmostx.BroadcastMode_BROADCAST_MODE_SYNC,
		})
		if err != nil {
			return fmt.Errorf("failed to broadcast tx (retry): %w", err)
		}
	}

	if resp.TxResponse != nil && resp.TxResponse.Code != 0 {
		return fmt.Errorf("tx failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
	}

	// Increment sequence for next tx
	if err := s.signer.IncrementSequence(s.keyName); err != nil {
		return fmt.Errorf("failed to increment sequence: %w", err)
	}

	return nil
}

// refreshSequence re-queries the account sequence from the consensus node
func (s *Streamer) refreshSequence(ctx context.Context) error {
	record, err := s.signer.Keyring().Key(s.keyName)
	if err != nil {
		return err
	}
	addr, err := record.GetAddress()
	if err != nil {
		return err
	}

	_, seqNum, err := user.QueryAccount(ctx, s.conn, s.encCfg.InterfaceRegistry, addr)
	if err != nil {
		return err
	}

	return s.signer.SetSequence(s.keyName, seqNum)
}
