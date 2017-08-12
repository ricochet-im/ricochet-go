package utils

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"errors"
	"crypto/rand"
)

const (
	InvalidPrivateKeyFileError = Error("InvalidPrivateKeyFileError")
	RICOCHET_KEY_SIZE = 1024
)

// Generate a private key for use
func GeneratePrivateKey() (*rsa.PrivateKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, RICOCHET_KEY_SIZE)
	if err != nil {
		return nil, errors.New("Could not generate key: " + err.Error())
	}
	privateKeyDer := x509.MarshalPKCS1PrivateKey(privateKey)
	return x509.ParsePKCS1PrivateKey(privateKeyDer)
}

// LoadPrivateKeyFromFile loads a private key from a file...
func LoadPrivateKeyFromFile(filename string) (*rsa.PrivateKey, error) {
	pemData, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return ParsePrivateKey(pemData)
}

// Convert a private key string to a usable private key
func ParsePrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, InvalidPrivateKeyFileError
	}

	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// turn a private key into storable string
func PrivateKeyToString(privateKey *rsa.PrivateKey) string {
	privateKeyBlock := pem.Block{
		Type: "RSA PRIVATE KEY",
		Headers: nil,
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	return string(pem.EncodeToMemory(&privateKeyBlock))
}
