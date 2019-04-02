package dkgnode

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/torusresearch/torus-public/secp256k1"

	"github.com/intel-go/fastjson"
	"github.com/osamingo/jsonrpc"
	cache "github.com/patrickmn/go-cache"
	tmquery "github.com/tendermint/tendermint/libs/pubsub/query"
	"github.com/tidwall/gjson"
	"github.com/torusresearch/torus-public/common"
	"github.com/torusresearch/torus-public/logging"
)

func (h CommitmentRequestHandler) ServeJSONRPC(c context.Context, params *fastjson.RawMessage) (interface{}, *jsonrpc.Error) {
	var p CommitmentRequestParams
	if err := jsonrpc.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	timestamp := p.Timestamp
	tokenCommitment := p.TokenCommitment
	verifierIdentifier := p.VerifierIdentifier

	// check if message prefix is correct
	if p.MessagePrefix != "mug00" {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Incorrect message prefix"}
	}

	// check if timestamp has expired
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not parse timestamp"}
	}
	if h.TimeNow().After(time.Unix(sec+60, 0)) {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Expired token (> 60 seconds)"}
	}

	// check if tokenCommitment has been seen before for that particular verifierIdentifier
	if h.suite.CacheSuite.TokenCaches[verifierIdentifier] == nil {
		h.suite.CacheSuite.TokenCaches[verifierIdentifier] = cache.New(cache.NoExpiration, 10*time.Minute)
	}
	tokenCache := h.suite.CacheSuite.TokenCaches[verifierIdentifier]
	_, found := tokenCache.Get(tokenCommitment)
	if found {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Duplicate token found"}
	}
	tokenCache.Set(string(tokenCommitment), true, 1*time.Minute)

	// sign data
	commitmentRequestResultData := CommitmentRequestResultData{
		p.MessagePrefix,
		p.TokenCommitment,
		p.TempPubX,
		p.TempPubY,
		p.Timestamp,
		p.VerifierIdentifier,
		strconv.FormatInt(time.Now().Unix(), 10),
	}

	sig := ECDSASign([]byte(commitmentRequestResultData.ToString()), h.suite.EthSuite.NodePrivateKey)

	res := CommitmentRequestResult{
		Signature: ECDSASigToHex(sig),
		Data:      commitmentRequestResultData.ToString(),
		NodePubX:  h.suite.EthSuite.NodePublicKey.X.Text(16),
		NodePubY:  h.suite.EthSuite.NodePublicKey.Y.Text(16),
	}
	return res, nil
}

func (h PingHandler) ServeJSONRPC(c context.Context, params *fastjson.RawMessage) (interface{}, *jsonrpc.Error) {

	var p PingParams
	if err := jsonrpc.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	return PingResult{
		Message: h.ethSuite.NodeAddress.Hex(),
	}, nil
}

// checks id for assignment and then the auth token for verification
// returns the node's share of the user's key
func (h ShareRequestHandler) ServeJSONRPC(c context.Context, params *fastjson.RawMessage) (interface{}, *jsonrpc.Error) {
	var p ShareRequestParams
	if err := jsonrpc.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// verify token validity against verifier
	h.suite.DefaultVerifier.Verify(params)
	// Validate signatures
	var validSignatures []ValidatedNodeSignature
	for i := 0; i < len(p.NodeSignatures); i++ {
		nodeRef, err := p.NodeSignatures[i].NodeValidation(h.suite)
		if err == nil {
			validSignatures = append(validSignatures, ValidatedNodeSignature{
				p.NodeSignatures[i],
				*nodeRef.Index,
			})
		} else {
			fmt.Println(err)
		}
	}
	// Check if we have threshold number of signatures
	if len(validSignatures) < h.suite.Config.Threshold {
		return nil, &jsonrpc.Error{Code: 32602, Message: "Internal error", Data: "Not enough valid signatures. Only " + strconv.Itoa(len(validSignatures)) + "valid signatures found."}
	}
	// Find common data string, and filter valid signatures on the wrong data
	// this is to prevent nodes from submitting valid signatures on wrong data
	commonDataMap := make(map[string]int)
	for i := 0; i < len(validSignatures); i++ {
		var commitmentRequestResultData CommitmentRequestResultData
		commitmentRequestResultData.FromString(validSignatures[i].Data)
		stringData := commitmentRequestResultData.MessagePrefix + "|" +
			commitmentRequestResultData.TokenCommitment + "|" +
			commitmentRequestResultData.Timestamp + "|" +
			commitmentRequestResultData.VerifierIdentifier
		commonDataMap[stringData]++
	}
	var commonDataString string
	var commonDataCount int
	for k, v := range commonDataMap {
		if v > commonDataCount {
			commonDataString = k
		}
	}
	var validCommonSignatures []ValidatedNodeSignature
	for i := 0; i < len(validSignatures); i++ {
		var commitmentRequestResultData CommitmentRequestResultData
		commitmentRequestResultData.FromString(validSignatures[i].Data)
		stringData := commitmentRequestResultData.MessagePrefix + "|" +
			commitmentRequestResultData.TokenCommitment + "|" +
			commitmentRequestResultData.Timestamp + "|" +
			commitmentRequestResultData.VerifierIdentifier
		if stringData == commonDataString {
			validCommonSignatures = append(validCommonSignatures, validSignatures[i])
		}
	}
	if len(validCommonSignatures) < h.suite.Config.Threshold {
		return nil, &jsonrpc.Error{Code: 32602, Message: "Internal error", Data: "Not enough valid signatures on the same data, " + strconv.Itoa(len(validCommonSignatures)) + " valid signatures."}
	}
	var commonData = strings.Split(commonDataString, "|")
	var commonTokenCommitment = commonData[1]
	var commonTimestamp = commonData[2]
	var commonVerifierIdentifier = commonData[3]

	// Lookup verifier
	verifier, err := h.suite.DefaultVerifier.Lookup(commonVerifierIdentifier)
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: err.Error()}
	}

	// verify that hash of token = tokenCommitment
	var cleanedToken = verifier.CleanToken(p.Token)
	if hex.EncodeToString(secp256k1.Keccak256([]byte(cleanedToken))) != commonTokenCommitment {
		fmt.Println("hex", hex.EncodeToString(secp256k1.Keccak256([]byte(cleanedToken))))
		fmt.Println("tokcom", commonTokenCommitment)
		return nil, &jsonrpc.Error{Code: 32602, Message: "Internal error", Data: "Token commitment and token are not compatible"}
	}

	// verify token timestamp
	sec, err := strconv.ParseInt(commonTimestamp, 10, 64)
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not parse timestamp"}
	}
	if h.TimeNow().After(time.Unix(sec+60, 0)) {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Expired token (> 60 seconds)"}
	}

	// Here we verify the identity of the user
	identityVerified, err := h.suite.DefaultVerifier.Verify(params)
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32602, Message: "Invalid params", Data: "oauth is invalid, err: " + err.Error()}
	}
	if !identityVerified {
		// TELEMETRY HERE?
		// TODO: @zhen / @len -> what should be the error message?
		return nil, &jsonrpc.Error{Code: 32602, Message: "Invalid identity", Data: "oauth is invalid, err: " + err.Error()}
	}

	tmpSi, found := h.suite.CacheSuite.CacheInstance.Get("Si_MAPPING")
	if !found {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not get si mapping here, not found"}
	}
	siMapping := tmpSi.(map[int]SiStore)

	res, err := h.suite.BftSuite.BftRPC.ABCIQuery("GetEmailIndex", []byte(p.ID))
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not get email index here: " + err.Error()}
	}

	userIndex, err := strconv.ParseUint(string(res.Response.Value), 10, 64)
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Cannot parse uint for user index here: " + err.Error()}
	}

	tmpInt := siMapping[int(userIndex)].Value

	return ShareRequestResult{
		Index:    siMapping[int(userIndex)].Index,
		HexShare: tmpInt.Text(16),
	}, nil
}

// assigns a user a secret, returns the same index if the user has been previously assigned
func (h SecretAssignHandler) ServeJSONRPC(c context.Context, params *fastjson.RawMessage) (interface{}, *jsonrpc.Error) {
	randomInt := rand.Int()
	var p SecretAssignParams
	if err := jsonrpc.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	logging.Debug("CHECKING IF EMAIL IS PROVIDED")
	// no email provided TODO: email validation/ ddos protection
	if p.Email == "" {
		return nil, &jsonrpc.Error{Code: 32602, Message: "Input error", Data: "Email is empty"}
	}
	logging.Debug("CHECKING IF CAN GET EMAIL ADDRESS")

	// try to get get email index
	logging.Debug("CHECKING IF ALREADY ASSIGNED")
	previouslyAssignedIndex, ok := h.suite.ABCIApp.state.EmailMapping[p.Email]
	// already assigned
	if ok {
		//create users publicKey
		logging.Debugf("previouslyAssignedIndex: %d", previouslyAssignedIndex)
		finalUserPubKey, err := retrieveUserPubKey(h.suite, int(previouslyAssignedIndex))
		if err != nil {
			return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "could not retrieve secret from previously assigned index, please try again err, " + err.Error()}
		}

		//form address eth
		addr, err := common.PointToEthAddress(*finalUserPubKey)
		if err != nil {
			logging.Error("derived user pub key has issues with address")
			return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error"}
		}

		return SecretAssignResult{
			ShareIndex: int(previouslyAssignedIndex),
			PubShareX:  finalUserPubKey.X.Text(16),
			PubShareY:  finalUserPubKey.Y.Text(16),
			Address:    addr.String(),
		}, nil

		// QUESTION(TEAM) -> THIS NEVER WAS REACHED, can you tell me why is it here?
		// return nil, &jsonrpc.Error{Code: 32602, Message: "Input error", Data: "Email exists"}
	}

	//if all indexes have been assigned, bounce request. threshold at 20% TODO: Make  percentage variable
	if h.suite.ABCIApp.state.LastCreatedIndex < h.suite.ABCIApp.state.LastUnassignedIndex+20 {
		return nil, &jsonrpc.Error{Code: 32604, Message: "System is under heavy load for assignments, please try again later"}
	}

	logging.Debug("CHECKING IF REACHED NEW ASSIGNMENT")
	// new assignment

	// broadcast assignment transaction
	hash, err := h.suite.BftSuite.BftRPC.Broadcast(DefaultBFTTxWrapper{&AssignmentBFTTx{Email: p.Email, Epoch: h.suite.ABCIApp.state.Epoch}})
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Unable to broadcast: " + err.Error()}
	}

	logging.Debugf("CHECKING IF SUBSCRIBE TO UPDATES %d", randomInt)
	// subscribe to updates
	query := tmquery.MustParse("tx.hash='" + hash.String() + "'")
	logging.Debugf("BFTWS:, hashstring %s %d", hash.String(), randomInt)
	logging.Debugf("BFTWS: querystring %s %d", query.String(), randomInt)

	logging.Debugf("CHECKING IF GOT RESPONSES %d", randomInt)
	// wait for block to be committed
	var assignedIndex uint
	responseCh, err := h.suite.BftSuite.RegisterQuery(query.String(), 1)
	if err != nil {
		logging.Debugf("BFTWS: could not register query, %s", query.String())
	}

	for e := range responseCh {
		logging.Debugf("BFTWS: gjson: %s %d", gjson.GetBytes(e, "query").String(), randomInt)
		logging.Debugf("BFTWS: queryString %s %d", query.String(), randomInt)
		if gjson.GetBytes(e, "query").String() != query.String() {
			continue
		}
		res, err := h.suite.BftSuite.BftRPC.ABCIQuery("GetEmailIndex", []byte(p.Email))
		if err != nil {
			return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Failed to check if email exists after assignment: " + err.Error()}
		}
		if string(res.Response.Value) == "" {
			return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Failed to find email after it has been assigned"}
		}
		assignedIndex64, err := strconv.ParseUint(string(res.Response.Value), 10, 64)
		assignedIndex = uint(assignedIndex64)
		if err != nil {
			return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Failed to parse uint for returned assignment index: " + fmt.Sprint(res) + " Error: " + err.Error()}
		}
		logging.Debugf("EXITING RESPONSES LISTENER %d", randomInt)
		break
	}

	// TODO: after ws response has returned as the secret mapping could have changed, should be initializable anywhere
	tmpSecretMAPPING, found := h.suite.CacheSuite.CacheInstance.Get("Secret_MAPPING")
	if !found {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not get sec mapping here after ws reply"}
	}
	secretMapping := tmpSecretMAPPING.(map[int]SecretStore)
	if err := jsonrpc.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	if secretMapping[int(assignedIndex)].Secret == nil {
		logging.Debugf("LOOKUP: secretmapping %v", secretMapping)
		logging.Debug("LOOKUP: SHOULD BE ERROR")
		// return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not retrieve secret from secret mapping, please try again"}
	}

	//create users publicKey
	finalUserPubKey, err := retrieveUserPubKey(h.suite, int(assignedIndex))
	if err != nil {
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error", Data: "Could not retrieve user public key through query, please try again, err " + err.Error()}
	}

	//form address eth
	addr, err := common.PointToEthAddress(*finalUserPubKey)
	if err != nil {
		logging.Debug("derived user pub key has issues with address")
		return nil, &jsonrpc.Error{Code: 32603, Message: "Internal error"}
	}

	return SecretAssignResult{
		ShareIndex: int(assignedIndex),
		PubShareX:  finalUserPubKey.X.Text(16),
		PubShareY:  finalUserPubKey.Y.Text(16),
		Address:    addr.String(),
	}, nil
}
