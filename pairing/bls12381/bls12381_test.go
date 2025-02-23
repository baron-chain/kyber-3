package bls12381

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"runtime"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/internal/test"
	"go.dedis.ch/kyber/v4/pairing"
	circl "go.dedis.ch/kyber/v4/pairing/bls12381/circl"
	kilic "go.dedis.ch/kyber/v4/pairing/bls12381/kilic"
	"go.dedis.ch/kyber/v4/sign/bls"
	"go.dedis.ch/kyber/v4/sign/tbls"
	"go.dedis.ch/kyber/v4/util/random"
	"go.dedis.ch/kyber/v4/xof/blake2xb"
	"gopkg.in/yaml.v3"
)

var (
	_, filename, _, _ = runtime.Caller(0)
	basepath          = filepath.Dir(filename)

	deserializationG1Tests = filepath.Join(basepath, "deserialization_tests/G1/*")
	deserializationG2Tests = filepath.Join(basepath, "deserialization_tests/G2/*")
)

func TestScalarEndianess(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	seed := "TestScalarEndianess"
	rng := blake2xb.New([]byte(seed))

	// byte 1 and 8
	var one, eight byte
	one |= 1
	eight |= 8

	for _, suite := range suites {
		// Select a random element
		s := suite.G1().Scalar().Pick(rng)
		sInv := s.Clone().Inv(s)

		// We expect the multiplicative neutral 1
		neutral := s.Mul(s, sInv)
		byteNeutral, err := neutral.MarshalBinary()
		require.NoError(t, err)

		if neutral.ByteOrder() == kyber.LittleEndian {
			require.Equal(t, byteNeutral[0], eight)
		} else {
			require.Equal(t, byteNeutral[len(byteNeutral)-1], one)
		}
	}
}

func TestZKCryptoVectorsG1Compressed(t *testing.T) {
	type Test struct {
		Input struct {
			PubKeyHexStr string `yaml:"pubkey"`
		}
		IsValidPredicate *bool `yaml:"output"`
	}
	tests, err := filepath.Glob(deserializationG1Tests)
	require.NoError(t, err)

	for _, testPath := range tests {
		t.Run(testPath, func(t *testing.T) {
			testFile, err := os.Open(testPath)
			require.NoError(t, err)
			test := Test{}
			err = yaml.NewDecoder(testFile).Decode(&test)
			require.NoError(t, testFile.Close())
			require.NoError(t, err)
			testCaseValid := test.IsValidPredicate != nil
			byts, err := hex.DecodeString(test.Input.PubKeyHexStr)
			if err != nil && testCaseValid {
				panic(err)
			}

			// Test kilic
			g := kilic.NullG1()
			err = g.UnmarshalBinary(byts)
			if err == nil && !testCaseValid {
				panic("Kilic: err should not be nil")
			}
			if err != nil && testCaseValid {
				panic("Kilic: err should be nil")
			}

			// Test circl
			g2 := circl.G1Elt{}
			err = g2.UnmarshalBinary(byts)
			if err == nil && !testCaseValid {
				panic("Circl: err should not be nil")
			}
			if err != nil && testCaseValid {
				panic("Circl: err should be nil")
			}
		})
	}
}

func TestZKCryptoVectorsG2Compressed(t *testing.T) {
	type Test struct {
		Input struct {
			SignatureHexStr string `yaml:"signature"`
		}
		IsValidPredicate *bool `yaml:"output"`
	}
	tests, err := filepath.Glob(deserializationG2Tests)
	require.NoError(t, err)

	for _, testPath := range tests {
		t.Run(testPath, func(t *testing.T) {
			testFile, err := os.Open(testPath)
			require.NoError(t, err)
			test := Test{}
			err = yaml.NewDecoder(testFile).Decode(&test)
			require.NoError(t, testFile.Close())
			require.NoError(t, err)
			testCaseValid := test.IsValidPredicate != nil
			byts, err := hex.DecodeString(test.Input.SignatureHexStr)
			if err != nil && testCaseValid {
				panic(err)
			}

			// Test kilic
			g := kilic.NullG2()
			err = g.UnmarshalBinary(byts)
			if err == nil && !testCaseValid {
				panic("Kilic: err should not be nil")
			}
			if err != nil && testCaseValid {
				panic("Kilic: err should be nil")
			}

			// Test circl
			g2 := circl.G2Elt{}
			err = g2.UnmarshalBinary(byts)
			if err == nil && !testCaseValid {
				panic("Circ: err should not be nil")
			}
			if err != nil && testCaseValid {
				panic("Circl: err should be nil")
			}
		})
	}
}

// Apply a generic set of validation tests to a cryptographic Group,
// using a given source of [pseudo-]randomness.
//
// Returns a log of the pseudorandom Points produced in the test,
// for comparison across alternative implementations
// that are supposed to be equivalent.
//
//nolint:gocyclo,cyclop // complete test
func testGroup(t *testing.T, g kyber.Group, rand cipher.Stream) []kyber.Point {
	t.Logf("\nTesting group '%s': %d-byte Point, %d-byte Scalar\n",
		g.String(), g.PointLen(), g.ScalarLen())

	points := make([]kyber.Point, 0)
	ptmp := g.Point()
	stmp := g.Scalar()
	pzero := g.Point().Null()
	szero := g.Scalar().Zero()
	sone := g.Scalar().One()

	// Do a simple Diffie-Hellman test
	s1 := g.Scalar().Pick(rand)
	s2 := g.Scalar().Pick(rand)
	if s1.Equal(szero) {
		t.Fatalf("first secret is scalar zero %v", s1)
	}
	if s2.Equal(szero) {
		t.Fatalf("second secret is scalar zero %v", s2)
	}
	if s1.Equal(s2) {
		t.Fatalf("not getting unique secrets: picked %s twice", s1)
	}

	gen := g.Point().Base()
	points = append(points, gen)

	// Sanity-check relationship between addition and multiplication
	p1 := g.Point().Add(gen, gen)
	p2 := g.Point().Mul(stmp.SetInt64(2), nil)
	if !p1.Equal(p2) {
		t.Fatalf("multiply by two doesn't work: %v == %v (+) %[2]v != %[2]v (x) 2 == %v", p1, gen, p2)
	}
	p1.Add(p1, p1)
	p2.Mul(stmp.SetInt64(4), nil)
	if !p1.Equal(p2) {
		t.Fatalf("multiply by four doesn't work: %v (+) %[1]v != %v (x) 4 == %v",
			g.Point().Add(gen, gen), gen, p2)
	}
	points = append(points, p1)

	// Find out if this curve has a prime order:
	// if the curve does not offer a method IsPrimeOrder,
	// then assume that it is.
	type canCheckPrimeOrder interface {
		IsPrimeOrder() bool
	}
	primeOrder := true
	if gpo, ok := g.(canCheckPrimeOrder); ok {
		primeOrder = gpo.IsPrimeOrder()
	}

	// Verify additive and multiplicative identities of the generator.
	// TODO: Check GT exp
	ptmp.Mul(stmp.SetInt64(-1), nil).Add(ptmp, gen)
	if !ptmp.Equal(pzero) {
		t.Fatalf("generator additive identity doesn't work: (scalar -1 %v) %v (x) -1 (+) %v = %v != %v the group point identity",
			stmp.SetInt64(-1), ptmp.Mul(stmp.SetInt64(-1), nil), gen, ptmp.Mul(stmp.SetInt64(-1), nil).Add(ptmp, gen), pzero)
	}
	// secret.Inv works only in prime-order groups
	if primeOrder {
		ptmp.Mul(stmp.SetInt64(2), nil).Mul(stmp.Inv(stmp), ptmp)
		if !ptmp.Equal(gen) {
			t.Fatalf("generator multiplicative identity doesn't work:\n%v (x) %v = %v\n%[3]v (x) %v = %v",
				ptmp.Base().String(), stmp.SetInt64(2).String(),
				ptmp.Mul(stmp.SetInt64(2), nil).String(),
				stmp.Inv(stmp).String(),
				ptmp.Mul(stmp.SetInt64(2), nil).Mul(stmp.Inv(stmp), ptmp).String())
		}
	}

	p1.Mul(s1, gen)
	p2.Mul(s2, gen)
	if p1.Equal(p2) {
		t.Fatalf("encryption isn't producing unique points: %v (x) %v == %v (x) %[2]v == %[4]v", s1, gen, s2, p1)
	}
	points = append(points, p1)

	dh1 := g.Point().Mul(s2, p1)
	dh2 := g.Point().Mul(s1, p2)
	if !dh1.Equal(dh2) {
		t.Fatalf("Diffie-Hellman didn't work: %v == %v (x) %v != %v (x) %v == %v", dh1, s2, p1, s1, p2, dh2)
	}
	points = append(points, dh1)

	// Test secret inverse to get from dh1 back to p1
	if primeOrder {
		ptmp.Mul(g.Scalar().Inv(s2), dh1)
		if !ptmp.Equal(p1) {
			t.Fatalf("Scalar inverse didn't work: %v != (-)%v (x) %v == %v", p1, s2, dh1, ptmp)
		}
	}

	// Zero and One identity secrets
	if !ptmp.Mul(szero, dh1).Equal(pzero) {
		t.Fatalf("Encryption with secret=0 didn't work: %v (x) %v == %v != %v", szero, dh1, ptmp, pzero)
	}
	if !ptmp.Mul(sone, dh1).Equal(dh1) {
		t.Fatalf("Encryption with secret=1 didn't work: %v (x) %v == %v != %[2]v", sone, dh1, ptmp)
	}

	// Additive homomorphic identities
	ptmp.Add(p1, p2)
	stmp.Add(s1, s2)
	pt2 := g.Point().Mul(stmp, gen)
	if !pt2.Equal(ptmp) {
		t.Fatalf("Additive homomorphism doesn't work: %v + %v == %v, %[3]v (x) %v == %v != %v == %v (+) %v",
			s1, s2, stmp, gen, pt2, ptmp, p1, p2)
	}
	ptmp.Sub(p1, p2)
	stmp.Sub(s1, s2)
	pt2.Mul(stmp, gen)
	if !pt2.Equal(ptmp) {
		t.Fatalf("Additive homomorphism doesn't work: %v - %v == %v, %[3]v (x) %v == %v != %v == %v (-) %v",
			s1, s2, stmp, gen, pt2, ptmp, p1, p2)
	}
	st2 := g.Scalar().Neg(s2)
	st2.Add(s1, st2)
	if !stmp.Equal(st2) {
		t.Fatalf("Scalar.Neg doesn't work: -%v == %v, %[2]v + %v == %v != %v",
			s2, g.Scalar().Neg(s2), s1, st2, stmp)
	}
	pt2.Neg(p2).Add(pt2, p1)
	if !pt2.Equal(ptmp) {
		t.Fatalf("Point.Neg doesn't work: (-)%v == %v, %[2]v (+) %v == %v != %v",
			p2, g.Point().Neg(p2), p1, pt2, ptmp)
	}

	// Multiplicative homomorphic identities
	stmp.Mul(s1, s2)
	if !ptmp.Mul(stmp, gen).Equal(dh1) {
		t.Fatalf("Multiplicative homomorphism doesn't work: %v * %v == %v, %[2]v (x) %v == %v != %v",
			s1, s2, stmp, gen, ptmp, dh1)
	}
	if primeOrder {
		st2.Inv(s2)
		st2.Mul(st2, stmp)
		if !st2.Equal(s1) {
			t.Fatalf("Scalar division doesn't work: %v^-1 * %v == %v * %[2]v == %[4]v != %v",
				s2, stmp, g.Scalar().Inv(s2), st2, s1)
		}
		st2.Div(stmp, s2)
		if !st2.Equal(s1) {
			t.Fatalf("Scalar division doesn't work: %v / %v == %v != %v",
				stmp, s2, st2, s1)
		}
	}

	pick := func(rand cipher.Stream) (p kyber.Point) {
		defer func() {
			// TODO implement Pick for GT
		}()
		p = g.Point().Pick(rand)
		return
	}

	// Test randomly picked points
	last := gen
	for i := 0; i < 5; i++ {
		// TODO fork kyber and make that an interface
		rgen := pick(rand)
		if rgen.Equal(last) {
			t.Fatalf("Pick() not producing unique points: got %v twice", rgen)
		}
		last = rgen

		ptmp.Mul(stmp.SetInt64(-1), rgen).Add(ptmp, rgen)
		if !ptmp.Equal(pzero) {
			t.Fatalf("random generator fails additive identity: %v (x) %v == %v, %v (+) %[3]v == %[5]v != %v",
				g.Scalar().SetInt64(-1), rgen, g.Point().Mul(g.Scalar().SetInt64(-1), rgen),
				rgen, g.Point().Mul(g.Scalar().SetInt64(-1), rgen), pzero)
		}
		if primeOrder {
			ptmp.Mul(stmp.SetInt64(2), rgen).Mul(stmp.Inv(stmp), ptmp)
			if !ptmp.Equal(rgen) {
				t.Fatalf("random generator fails multiplicative identity: %v (x) (2 (x) %v) == %v != %[2]v",
					stmp, rgen, ptmp)
			}
		}
		points = append(points, rgen)
	}

	// Test encoding and decoding
	buf := new(bytes.Buffer)
	for i := 0; i < 5; i++ {
		buf.Reset()
		s := g.Scalar().Pick(rand)
		if _, err := s.MarshalTo(buf); err != nil {
			t.Fatalf("encoding of secret fails: %s", err.Error())
		}
		if _, err := stmp.UnmarshalFrom(buf); err != nil {
			t.Fatalf("decoding of secret fails: %s", err.Error())
		}
		if !stmp.Equal(s) {
			t.Fatalf("decoding produces different secret than encoded")
		}

		buf.Reset()
		p := pick(rand)
		if _, err := p.MarshalTo(buf); err != nil {
			t.Fatalf("encoding of point fails: %s", err)
		}
		if _, err := ptmp.UnmarshalFrom(buf); err != nil {
			t.Fatalf("decoding of point fails: %s", err.Error())
		}

		if !ptmp.Equal(p) {
			t.Fatalf("decoding produces different point than encoded")
		}
	}

	// Test that we can marshal/ unmarshal null point
	pzero = g.Point().Null()
	b, _ := pzero.MarshalBinary()
	repzero := g.Point()
	err := repzero.UnmarshalBinary(b)
	if err != nil {
		t.Fatalf("Could not unmarshall binary %v: %v", b, err)
	}

	return points
}

// GroupTest applies a generic set of validation tests to a cryptographic Group.
func GroupTest(t *testing.T, g kyber.Group) {
	testGroup(t, g, random.New())
}

func TestKyberG1(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		GroupTest(t, suite.G1())
	}
}

func TestKyberG2(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		GroupTest(t, suite.G2())
	}
}

func TestKyberPairingG2(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, s := range suites {
		a := s.G1().Scalar().Pick(s.RandomStream())
		b := s.G2().Scalar().Pick(s.RandomStream())
		aG := s.G1().Point().Mul(a, nil)
		bH := s.G2().Point().Mul(b, nil)
		ab := s.G1().Scalar().Mul(a, b)
		abG := s.G1().Point().Mul(ab, nil)
		// e(aG, bG) = e(G,H)^(ab)
		p1 := s.Pair(aG, bH)
		// e((ab)G,H) = e(G,H)^(ab)
		p2 := s.Pair(abG, s.G2().Point().Base())
		require.True(t, p1.Equal(p2))
		require.True(t, s.ValidatePairing(aG, bH, abG.Clone(), s.G2().Point().Base()))

		pRandom := s.Pair(aG, s.G2().Point().Pick(s.RandomStream()))
		require.False(t, p1.Equal(pRandom))
		pRandom = s.Pair(s.G1().Point().Pick(s.RandomStream()), bH)
		require.False(t, p1.Equal(pRandom))
	}
}

func TestRacePairings(_ *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, s := range suites {
		a := s.G1().Scalar().Pick(s.RandomStream())
		aG := s.G1().Point().Mul(a, nil)
		B := s.G2().Point().Pick(s.RandomStream())
		aB := s.G2().Point().Mul(a, B.Clone())
		wg := sync.WaitGroup{}
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				//  e(p1,p2) =?= e(inv1^-1, inv2^-1)
				s.ValidatePairing(aG, B, s.G1().Point(), aB)
				wg.Done()
			}()
		}
		wg.Wait()
	}
}

func TestKyberBLSG2(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		scheme := bls.NewSchemeOnG2(suite)
		test.SchemeTesting(t, scheme)
	}
}

func TestKyberBLSG1(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		scheme := bls.NewSchemeOnG1(suite)
		test.SchemeTesting(t, scheme)
	}
}

func TestKyberThresholdG2(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		tscheme := tbls.NewThresholdSchemeOnG2(suite)
		test.ThresholdTest(t, suite.G1(), tscheme)
	}
}

func TestKyberThresholdG1(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		tscheme := tbls.NewThresholdSchemeOnG1(suite)
		test.ThresholdTest(t, suite.G2(), tscheme)
	}
}

func TestIsValidGroup(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		p1 := suite.G1().Point().Pick(random.New())
		p2 := suite.G1().Point().Pick(random.New())

		require.True(t, p1.(kyber.SubGroupElement).IsInCorrectGroup())
		require.True(t, p2.(kyber.SubGroupElement).IsInCorrectGroup())
	}
}

func newElement(suite pairing.Suite) kyber.Scalar {
	return suite.G1().Scalar()
}
func newG1(suite pairing.Suite) kyber.Point {
	return suite.G1().Point().Base()
}
func newG2(suite pairing.Suite) kyber.Point {
	return suite.G2().Point().Base()
}
func pair(suite pairing.Suite, a, b kyber.Point) kyber.Point {
	return suite.Pair(a, b)
}

func TestBasicPairing(t *testing.T) {
	suites := []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, suite := range suites {
		// we test a * b = c + d
		a := newElement(suite).Pick(random.New())
		b := newElement(suite).Pick(random.New())
		c := newElement(suite).Pick(random.New())
		d := newElement(suite).Sub(newElement(suite).Mul(a, b), c)

		// check in the clear
		ab := newElement(suite).Mul(a, b)
		cd := newElement(suite).Add(c, d)
		require.True(t, ab.Equal(cd))

		// check in the exponent now with the following
		// e(aG1,bG2) = e(cG1,G2) * e(G1,dG2) <=>
		// e(G1,G2)^(a*b) = e(G1,G2)^c * e(G1,G2)^d
		// e(G1,G2)^(a*b) = e(G1,G2)^(c + d)
		aG := newG1(suite).Mul(a, nil)
		bG := newG2(suite).Mul(b, nil)
		left := pair(suite, aG, bG)

		cG := newG1(suite).Mul(c, nil)
		right1 := pair(suite, cG, newG2(suite))
		dG := newG2(suite).Mul(d, nil)
		right2 := pair(suite, newG1(suite), dG)
		right := suite.GT().Point().Add(right1, right2)
		require.True(t, left.Equal(right))

		// Test if addition works in GT
		mright := right.Clone().Neg(right)
		res := mright.Add(mright, right)
		require.True(t, res.Equal(suite.GT().Point().Null()))

		// Test if Sub works in GT
		expZero := right.Clone().Sub(right, right)
		require.True(t, expZero.Equal(suite.GT().Point().Null()))

		//  Test if scalar mul works in GT
		// e(aG,G) == e(G,G)^a
		left = pair(suite, aG, suite.G2().Point().Base())
		right = pair(suite, suite.G1().Point().Base(), suite.G2().Point().Base())
		right = right.Mul(a, right)
		require.True(t, left.Equal(right))
	}
}

// Benchmarking
func BenchmarkPairingSeparate(bb *testing.B) {
	var suites = []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, s := range suites {
		bb.Run(s.G1().String(), func(bbb *testing.B) {
			a := s.G1().Scalar().Pick(s.RandomStream())
			b := s.G2().Scalar().Pick(s.RandomStream())
			aG := s.G1().Point().Mul(a, nil)
			bH := s.G2().Point().Mul(b, nil)
			ab := s.G1().Scalar().Mul(a, b)
			abG := s.G1().Point().Mul(ab, nil)
			bbb.ResetTimer()
			for i := 0; i < bbb.N; i++ {

				// e(aG, bG) = e(G,H)^(ab)
				p1 := s.Pair(aG, bH)
				// e((ab)G,H) = e(G,H)^(ab)
				p2 := s.Pair(abG, s.G2().Point().Base())
				if !p1.Equal(p2) {
					panic("aie")
				}
			}
		})
	}
}

func BenchmarkPairingInv(bb *testing.B) {
	var suites = []pairing.Suite{
		kilic.NewBLS12381Suite(),
		circl.NewSuiteBLS12381(),
	}

	for _, s := range suites {
		bb.Run(s.G1().String(), func(bbb *testing.B) {
			a := s.G1().Scalar().Pick(s.RandomStream())
			b := s.G2().Scalar().Pick(s.RandomStream())
			aG := s.G1().Point().Mul(a, nil)
			bH := s.G2().Point().Mul(b, nil)
			ab := s.G1().Scalar().Mul(a, b)
			abG := s.G1().Point().Mul(ab, nil)
			bbb.ResetTimer()
			for i := 0; i < bbb.N; i++ {
				// e(aG, bH) = e(G,H)^(ab)
				p1 := s.Pair(aG, bH)
				// e((ab)G,H) = e(G,H)^(ab)
				p2 := s.Pair(abG, s.G2().Point().Base())
				if !p1.Equal(p2) {
					panic("aie")
				}
			}
		})
	}
}

var (
	dataSize     = 32
	numSigs      = []int{1, 10, 100, 1000, 10000}
	curveOptions = []string{"kilic", "circl"}
)

// Used to avoid compiler optimizations
// https://www.practical-go-lessons.com/chap-34-benchmarks#:~:text=This%20variable%20is%20just%20here%20to%20avoid%20compiler%20optimization
var result interface{}

func BenchmarkKilic(b *testing.B) {
	BLSBenchmark(b, "kilic")
}

func BenchmarkCircl(b *testing.B) {
	BLSBenchmark(b, "circl")
}

func BLSBenchmark(b *testing.B, curveOption string) {
	b.Logf("----------------------")
	b.Logf("Payload to sign: %d bytes\n", dataSize)
	b.Logf("Numbers of signatures: %v\n", numSigs)
	b.Logf("Curve options: %v\n", curveOptions)
	b.Logf("----------------------")

	// Initialize all variables.
	msgData := make([]byte, dataSize)
	nBytes, err := rand.Read(msgData)
	if err != nil {
		panic(err)
	}
	if nBytes != dataSize {
		panic(fmt.Errorf("only read %d random bytes, but data size is %d", nBytes, dataSize))
	}

	randSource := random.New(rand.Reader)
	var suite pairing.Suite
	switch curveOption {
	case "kilic":
		suite = kilic.NewBLS12381Suite()
	case "circl":
		suite = circl.NewSuiteBLS12381()
	default:
		panic(fmt.Errorf("invalid curve option: %s", curveOption))
	}

	schemeOnG1 := bls.NewSchemeOnG1(suite)
	schemeOnG2 := bls.NewSchemeOnG2(suite)

	maxN := 1
	for _, s := range numSigs {
		if maxN < s {
			maxN = s
		}
	}

	privKeysOnG1 := make([]kyber.Scalar, maxN)
	privKeysOnG2 := make([]kyber.Scalar, maxN)
	pubKeysOnG1 := make([]kyber.Point, maxN)
	pubKeysOnG2 := make([]kyber.Point, maxN)
	sigsOnG1 := make([][]byte, maxN)
	sigsOnG2 := make([][]byte, maxN)

	for i := 0; i < maxN; i++ {
		privKeysOnG1[i], pubKeysOnG1[i] = schemeOnG1.NewKeyPair(randSource)
		sigsOnG1[i], err = schemeOnG1.Sign(privKeysOnG1[i], msgData)
		if err != nil {
			panic(err)
		}
		privKeysOnG2[i], pubKeysOnG2[i] = schemeOnG2.NewKeyPair(randSource)
		sigsOnG2[i], err = schemeOnG2.Sign(privKeysOnG2[i], msgData)
		if err != nil {
			panic(err)
		}
	}

	for _, n := range numSigs {
		// Benchmark aggregation of public keys
		b.Run(fmt.Sprintf("AggregatePublicKeys-G1 on %d signs", n), func(bb *testing.B) {
			for j := 0; j < bb.N; j++ {
				result = schemeOnG1.AggregatePublicKeys(pubKeysOnG1[:n]...)
			}
		})
		b.Run(fmt.Sprintf("AggregatePublicKeys-G2 on %d signs", n), func(bb *testing.B) {
			for j := 0; j < bb.N; j++ {
				result = schemeOnG2.AggregatePublicKeys(pubKeysOnG2[:n]...)
			}
		})

		// Benchmark aggregation of signatures
		b.Run(fmt.Sprintf("AggregateSign-G1 on %d signs", n), func(bb *testing.B) {
			for j := 0; j < bb.N; j++ {
				result, err = schemeOnG1.AggregateSignatures(sigsOnG1[:n]...)
				if err != nil {
					panic(err)
				}
			}
		})
		b.Run(fmt.Sprintf("AggregateSign-G1 on %d signs", n), func(bb *testing.B) {
			for j := 0; j < bb.N; j++ {
				result, err = schemeOnG2.AggregateSignatures(sigsOnG2[:n]...)
				if err != nil {
					panic(err)
				}
			}
		})
	}

	// Benchmark keygen
	b.Run("KeyGen-G1", func(bb *testing.B) {
		for j := 0; j < bb.N; j++ {
			result, _ = schemeOnG1.NewKeyPair(randSource)
		}
	})
	b.Run("KeyGen-G2", func(bb *testing.B) {
		for j := 0; j < bb.N; j++ {
			result, _ = schemeOnG2.NewKeyPair(randSource)
		}
	})

	// Benchmark sign
	b.Run("Sign-G1", func(bb *testing.B) {
		for j := 0; j < bb.N; j++ {
			result, err = schemeOnG1.Sign(privKeysOnG1[0], msgData)
			if err != nil {
				panic(err)
			}
		}
	})
	b.Run("Sign-G2", func(bb *testing.B) {
		for j := 0; j < bb.N; j++ {
			result, err = schemeOnG2.Sign(privKeysOnG2[0], msgData)
			if err != nil {
				panic(err)
			}
		}
	})

	// Benchmark verify
	b.Run("Verify-G1", func(bb *testing.B) {
		for j := 0; j < bb.N; j++ {
			err = schemeOnG1.Verify(pubKeysOnG1[0], msgData, sigsOnG1[0])
			if err != nil {
				panic(err)
			}
		}
	})
	b.Run("Verify-G2", func(bb *testing.B) {
		for j := 0; j < bb.N; j++ {
			err = schemeOnG2.Verify(pubKeysOnG2[0], msgData, sigsOnG2[0])
			if err != nil {
				panic(err)
			}
		}
	})
}
