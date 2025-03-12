package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var (
	trades      []map[string]interface{}
	mutex       sync.Mutex
	batchPeriod = 1 * time.Minute // Adjusted from 24h for testing
)

const (
	contractAddr  = "0x130548c20002412015b9dE28aE8Ed1Daa2874ea2"
	rpcURL        = "https://1rpc.io/sepolia"
	privateKeyHex = "0e64ca33c73045671342f0088800548716215a88d13880db02240b4717840588"
)

// Manually defined ABI for contract function and event
const tradeStorageABI = `[
	{"anonymous":false,"inputs":[
		{"indexed":true,"internalType":"uint256","name":"startTime","type":"uint256"},
		{"indexed":true,"internalType":"uint256","name":"endTime","type":"uint256"},
		{"indexed":false,"internalType":"string","name":"cid","type":"string"}
	],"name":"IntentsBatchIPFS","type":"event"},
	{"inputs":[
		{"internalType":"uint256","name":"startTime","type":"uint256"},
		{"internalType":"uint256","name":"endTime","type":"uint256"},
		{"internalType":"string","name":"cid","type":"string"}
	],"name":"intentBatchEmit","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

func main() {
	_ = godotenv.Load()

	r := gin.Default()

	// ✅ Enable CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"}, // Allow frontend origin
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.POST("/store-trade", storeTrade)

	log.Println("Backend running on port 8080")
	go batchAndUpload()
	r.Run(":8080")
}

func storeTrade(c *gin.Context) {
	var tradeData map[string]interface{}
	if err := c.BindJSON(&tradeData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	mutex.Lock()
	trades = append(trades, tradeData)
	mutex.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "Trade stored"})
}

func batchAndUpload() {
	for {
		time.Sleep(batchPeriod)

		mutex.Lock()
		if len(trades) == 0 {
			fmt.Println("No trades in this interval. Skipping file creation.")
			mutex.Unlock()
			continue
		}

		filePath := fmt.Sprintf("trades_%s.json", time.Now().Format("2006-01-02_15-04-05"))

		file, err := os.Create(filePath)
		if err != nil {
			log.Println("Error creating JSON file:", err)
			mutex.Unlock()
			continue
		}

		saveErr := json.NewEncoder(file).Encode(trades)
		file.Close()
		if saveErr != nil {
			log.Println("Error writing to JSON file:", saveErr)
			mutex.Unlock()
			continue
		}

		cid, err := uploadToPinata(filePath)
		if err != nil {
			log.Println("Error uploading to IPFS:", err)
			mutex.Unlock()
			continue
		}

		startTime := time.Now().Add(-batchPeriod).Unix()
		endTime := time.Now().Unix()
		err = emitTradeCID(startTime, endTime, cid)
		if err != nil {
			log.Println("Error emitting trade CID event:", err)
			log.Println("Trade data is still stored in:", filePath)
		} else {
			trades = nil
			fmt.Println("✅ Uploaded to IPFS, CID:", cid)
		}

		mutex.Unlock()
	}
}

func uploadToPinata(filePath string) (string, error) {
	apiKey := os.Getenv("PINATA_API_KEY")
	apiSecret := os.Getenv("PINATA_API_SECRET")

	if apiKey == "" || apiSecret == "" {
		return "", fmt.Errorf("missing Pinata API credentials")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filePath)
	if err != nil {
		return "", err
	}
	_, _ = io.Copy(part, file)

	writer.WriteField("pinataMetadata", `{"name": "`+filePath+`"}`)
	writer.WriteField("pinataOptions", `{"cidVersion": 1}`)
	writer.Close()

	req, err := http.NewRequest("POST", "https://api.pinata.cloud/pinning/pinFileToIPFS", body)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("pinata_api_key", apiKey)
	req.Header.Set("pinata_secret_api_key", apiSecret)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var responseData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return "", err
	}

	if cid, exists := responseData["IpfsHash"].(string); exists {
		fmt.Println("cid uploaded to ipfs:", cid)
		return cid, nil
	}

	return "", fmt.Errorf("failed to get CID from Pinata response")
}

func emitTradeCID(startTime, endTime int64, cid string) error {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}
	defer client.Close()

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	auth := bind.NewKeyedTransactor(privateKey)
	parsedABI, _ := abi.JSON(bytes.NewReader([]byte(tradeStorageABI)))
	contract := common.HexToAddress(contractAddr)

	data, err := parsedABI.Pack("intentBatchEmit", big.NewInt(startTime), big.NewInt(endTime), cid)
	if err != nil {
		return fmt.Errorf("failed to pack transaction data: %w", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		nonce, err := client.PendingNonceAt(context.Background(), auth.From)
		if err != nil {
			return fmt.Errorf("failed to get nonce: %w", err)
		}

		gasPrice, err := client.SuggestGasPrice(context.Background())
		if err != nil {
			return fmt.Errorf("failed to fetch suggested gas price: %w", err)
		}

		// Increase gas price by 10% to ensure faster inclusion
		gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(11))
		gasPrice = new(big.Int).Div(gasPrice, big.NewInt(10))

		tx := types.NewTransaction(nonce, contract, big.NewInt(0), 300000, gasPrice, data)
		signedTx, err := auth.Signer(auth.From, tx)
		if err != nil {
			return fmt.Errorf("failed to sign transaction: %w", err)
		}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			if attempt < 2 && isUnderpricedError(err) {
				fmt.Println("transaction underpriced, retrying in 10 seconds...")
				time.Sleep(10 * time.Second)
				continue
			}
			return fmt.Errorf("failed to send transaction: %w", err)
		}

		fmt.Println("trade cid event emitted, tx hash:", signedTx.Hash().Hex())
		go checkTransactionStatus(signedTx.Hash())

		// Wait for transaction to be mined
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		receipt, err := bind.WaitMined(ctx, client, signedTx)
		if err != nil {
			return fmt.Errorf("failed to wait for transaction to be mined: %w", err)
		}

		if receipt.Status == 1 {
			fmt.Println("✅ Transaction successful! Block:", receipt.BlockNumber)
			return nil
		} else {
			return fmt.Errorf("❌ Transaction failed in block %d", receipt.BlockNumber)
		}
	}

	return fmt.Errorf("failed after maximum retries")
}

func isUnderpricedError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "transaction underpriced") || strings.Contains(err.Error(), "replacement transaction underpriced"))
}

func checkTransactionStatus(txHash common.Hash) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Println("Error connecting to Ethereum RPC:", err)
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	receipt, err := client.TransactionReceipt(ctx, txHash)
	if err != nil {
		log.Println("Transaction not found or not yet mined:", err)
		return
	}

	if receipt.Status == 1 {
		fmt.Println("✅ Transaction successful! Block:", receipt.BlockNumber)
	} else {
		fmt.Println("❌ Transaction failed!")
	}
}
