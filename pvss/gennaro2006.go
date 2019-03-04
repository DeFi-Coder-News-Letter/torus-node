package pvss

// Secure Distributed Key Generation for Discrete-Log Based Cryptosystems

import (
	"math/big"

	"github.com/torusresearch/torus-public/common"
	"github.com/torusresearch/torus-public/secp256k1"
)

// Commit creates a public commitment polynomial for the h base point
func getCommitH(polynomial common.PrimaryPolynomial) []common.Point {
	commits := make([]common.Point, polynomial.Threshold)
	for i := range commits {
		commits[i] = common.BigIntToPoint(secp256k1.Curve.ScalarMult(&secp256k1.H.X, &secp256k1.H.Y, polynomial.Coeff[i].Bytes()))
	}
	// fmt.Println(commits[0].X.Text(16), commits[0].Y.Text(16), "commit0")
	// fmt.Println(commits[1].X.Text(16), commits[1].Y.Text(16), "commit1")
	return commits
}

// CreateSharesGen - Creating shares for gennaro DKG
func CreateSharesGen(nodes []common.Node, secret big.Int, threshold int) (*[]common.PrimaryShare, *[]common.PrimaryShare, *[]common.Point, *[]common.Point, error) {
	// generate two polynomials, one for pederson commitments
	polynomial := *generateRandomZeroPolynomial(secret, threshold)
	polynomialPrime := *generateRandomZeroPolynomial(*RandomBigInt(), threshold)

	// determine shares for polynomial with respect to basis point
	shares := getShares(polynomial, nodes)
	sharesPrime := getShares(polynomialPrime, nodes)

	// committing to polynomial
	pubPoly := getCommit(polynomial)
	pubPolyPrime := getCommitH(polynomialPrime)

	// create Ci
	Ci := make([]common.Point, threshold)
	for i := range pubPoly {
		Ci[i] = common.BigIntToPoint(secp256k1.Curve.Add(&pubPoly[i].X, &pubPoly[i].Y, &pubPolyPrime[i].X, &pubPolyPrime[i].Y))
	}

	return &shares, &sharesPrime, &pubPoly, &Ci, nil
}

// Verify Pederson commitment, Equation (4) in Gennaro 2006
func VerifyPedersonCommitment(share common.PrimaryShare, sharePrime common.PrimaryShare, ci []common.Point, index big.Int) bool {

	// committing to polynomial
	gSik := common.BigIntToPoint(secp256k1.Curve.ScalarBaseMult(share.Value.Bytes()))
	hSikPrime := common.BigIntToPoint(secp256k1.Curve.ScalarMult(&secp256k1.H.X, &secp256k1.H.Y, sharePrime.Value.Bytes()))

	//computing LHS
	lhs := common.BigIntToPoint(secp256k1.Curve.Add(&gSik.X, &gSik.Y, &hSikPrime.X, &hSikPrime.Y))

	//computing RHS
	rhs := common.Point{X: *new(big.Int).SetInt64(0), Y: *new(big.Int).SetInt64(0)}
	for i := range ci {
		jt := new(big.Int).Set(&index)
		jt.Exp(jt, new(big.Int).SetInt64(int64(i)), secp256k1.GeneratorOrder)
		polyValue := common.BigIntToPoint(secp256k1.Curve.ScalarMult(&ci[i].X, &ci[i].Y, jt.Bytes()))
		rhs = common.BigIntToPoint(secp256k1.Curve.Add(&rhs.X, &rhs.Y, &polyValue.X, &polyValue.Y))
	}

	if lhs.X.Cmp(&rhs.X) == 0 {
		return true
	}
	return false
}

// VerifyShare - verifies share against public polynomial
func VerifyShare(share common.PrimaryShare, pubPoly []common.Point, index big.Int) bool {

	lhs := common.BigIntToPoint(secp256k1.Curve.ScalarBaseMult(share.Value.Bytes()))

	// computing RHS
	rhs := common.Point{X: *new(big.Int).SetInt64(0), Y: *new(big.Int).SetInt64(0)}
	for i := range pubPoly {
		jt := new(big.Int).Set(&index)
		jt.Exp(jt, new(big.Int).SetInt64(int64(i)), secp256k1.GeneratorOrder)
		polyValue := common.BigIntToPoint(secp256k1.Curve.ScalarMult(&pubPoly[i].X, &pubPoly[i].Y, jt.Bytes()))
		rhs = common.BigIntToPoint(secp256k1.Curve.Add(&rhs.X, &rhs.Y, &polyValue.X, &polyValue.Y))
	}

	if lhs.X.Cmp(&rhs.X) == 0 {
		return true
	} else {
		return false
	}
}

// VerifyShareCommitment - checks if a dlog commitment matches the original pubpoly
func VerifyShareCommitment(shareCommitment common.Point, pubPoly []common.Point, index big.Int) bool {
	lhs := shareCommitment

	// computing RHS
	rhs := common.Point{X: *new(big.Int).SetInt64(0), Y: *new(big.Int).SetInt64(0)}
	for i := range pubPoly {
		jt := new(big.Int).Set(&index)
		jt.Exp(jt, new(big.Int).SetInt64(int64(i)), secp256k1.GeneratorOrder)
		polyValue := common.BigIntToPoint(secp256k1.Curve.ScalarMult(&pubPoly[i].X, &pubPoly[i].Y, jt.Bytes()))
		rhs = common.BigIntToPoint(secp256k1.Curve.Add(&rhs.X, &rhs.Y, &polyValue.X, &polyValue.Y))
	}

	if lhs.X.Cmp(&rhs.X) == 0 {
		return true
	} else {
		return false
	}
}
