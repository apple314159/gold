package gold

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/asn1"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	rsaBits = 2048
)

var (
	subjectAltName = []int{2, 5, 29, 17}

	notBefore = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter  = time.Date(2049, 12, 31, 23, 59, 59, 0, time.UTC)

	// cache
	webidL  = new(sync.Mutex)
	pkeyURI = map[string]string{}
)

func pkeyTypeNE(pkey interface{}) (t, n, e string) {
	switch pkey := pkey.(type) {
	//TODO: case *dsa.PublicKey
	case *rsa.PublicKey:
		t = "RSAPublicKey"
		n = fmt.Sprintf("%x", pkey.N)
		e = fmt.Sprintf("%d", pkey.E)
	}
	return
}

// WebIDTLSAuth - performs WebID-TLS authentication
func WebIDTLSAuth(tls *tls.ConnectionState) (uri string, err error) {
	claim := ""
	uri = ""
	err = nil

	if tls == nil || !tls.HandshakeComplete {
		DebugLog("WebID-TLS", "Not a TLS connection. TLS handshake failed")
		return
	}

	if len(tls.PeerCertificates) < 1 {
		DebugLog("WebID-TLS", "No client certificate found in the TLS request!")
		return
	}

	for _, x := range tls.PeerCertificates[0].Extensions {
		if !x.Id.Equal(subjectAltName) {
			continue
		}
		if len(x.Value) < 5 {
			continue
		}

		v := asn1.RawValue{}
		_, err = asn1.Unmarshal(x.Value, &v)
		if err == nil {
			claim = string(v.Bytes[2:])
		}
		if len(claim) == 0 {
			continue
		}

		pkey := tls.PeerCertificates[0].PublicKey
		t, n, e := pkeyTypeNE(pkey)
		if len(t) == 0 {
			continue
		}

		DebugLog("WebID-TLS", "Found public key from client containing WebID claim: "+claim)

		pkeyk := fmt.Sprint([]string{t, n, e})
		webidL.Lock()
		uri = pkeyURI[pkeyk]
		webidL.Unlock()
		if len(uri) > 0 {
			return
		}

		g := NewGraph(claim)
		err = g.LoadURI(claim)
		if err != nil {
			DebugLog("WebID-TLS", err.Error())
			return
		}

		for _, keyT := range g.All(NewResource(claim), ns.cert.Get("key"), nil) {
			DebugLog("WebID-TLS", "Found a public key in the profile.")
			for range g.All(keyT.Object, ns.rdf.Get("type"), ns.cert.Get(t)) {
				DebugLog("WebID-TLS", "Trying to match modulus found in cert:")
				DebugLog("WebID-TLS", n)
				for range g.All(keyT.Object, ns.cert.Get("modulus"), NewLiteral(n)) {
					goto matchModulus
				}
				for range g.All(keyT.Object, ns.cert.Get("modulus"), NewLiteralWithDatatype(n, NewResource("http://www.w3.org/2001/XMLSchema#hexBinary"))) {
					goto matchModulus
				}
			matchModulus:
				DebugLog("WebID-TLS", "Found a matching modulus in the profile.")
				for range g.All(keyT.Object, ns.cert.Get("exponent"), NewLiteral(e)) {
					goto matchExponent
				}
				for range g.All(keyT.Object, ns.cert.Get("exponent"), NewLiteralWithDatatype(e, NewResource("http://www.w3.org/2001/XMLSchema#int"))) {
					goto matchExponent
				}
			matchExponent:
				DebugLog("WebID-TLS", "Found a matching exponent in the profile.")
				DebugLog("WebID-TLS", "Authenticated claim URI: "+claim)
				uri = claim
				webidL.Lock()
				pkeyURI[pkeyk] = uri
				webidL.Unlock()
				return
			}
			DebugLog("WebID-TLS", "Could not find a certificate in the profile.")
		}
		DebugLog("WebID-TLS", "Could not find a certificate public key in the profile.")
	}
	return
}

// NewWebIDProfileWithKeys creates a WebID profile graph and corresponding keys
func NewWebIDProfileWithKeys(uri string) (*Graph, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, nil, err
	}

	profileURI := strings.Split(uri, "#")[0]
	userTerm := NewResource(uri)
	keyTerm := NewResource(profileURI + "#key")

	g := NewGraph(profileURI)
	g.AddTriple(userTerm, ns.cert.Get("key"), keyTerm)
	g.AddTriple(keyTerm, ns.rdf.Get("type"), ns.cert.Get("RSAPublicKey"))
	g.AddTriple(keyTerm, ns.cert.Get("modulus"), NewLiteralWithDatatype(fmt.Sprintf("%x", priv.N), NewResource("http://www.w3.org/2001/XMLSchema#hexBinary")))
	g.AddTriple(keyTerm, ns.cert.Get("exponent"), NewLiteral(fmt.Sprintf("%d", priv.E)))
	return g, priv, nil
}
