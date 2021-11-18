package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ed25519"

	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/proto/messages"
)

func TestMarshalLicense(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	assert.NoError(t, err)

	// Test when the keys match.
	licenseVerificationPublicKey = pubKey
	license := License{
		Terms: Terms{
			Customer:   "test",
			Seats:      5,
			ExpiryTime: time.Unix(1136239445, 0).UTC(), // Go reference timestamp
			Type:       Trial,
		},
	}

	licenseStr, err := license.Marshal(privKey)
	assert.NoError(t, err)

	parsed, err := ParseLicense(licenseStr)
	assert.NoError(t, err)

	assert.Equal(t, license.Terms, parsed.Terms)
	assert.True(t, parsed.Signed)

	// If the signing public key changes, the license should no longer verify.
	newPubKey, _, err := ed25519.GenerateKey(nil)
	assert.NoError(t, err)
	licenseVerificationPublicKey = newPubKey

	parsed, err = ParseLicense(licenseStr)
	assert.NoError(t, err)

	assert.False(t, parsed.Signed)
	licenseVerificationPublicKey = pubKey

	// If the supported version changes, the license should no longer parse.
	SupportedLicenseVersion = "changed"
	_, err = ParseLicense(licenseStr)
	assert.EqualError(t, err,
		fmt.Sprintf(licenseVersionMismatchTemplate, SupportedLicenseVersion, parsed.Version))
}

func TestCheckSeats(t *testing.T) {
	tests := []struct {
		name        string
		license     License
		usedSeats   int
		expWarnings []*messages.Message
		err         error
	}{
		{
			name: "testSeatsCustomerOk",
			license: License{
				Terms: Terms{
					Customer: "test",
					Type:     Customer,
					Seats:    10,
				},
			},
			usedSeats:   0,
			expWarnings: nil,
			err:         nil,
		},
		{
			name: "testSeatsCustomerWarn",
			license: License{
				Terms: Terms{
					Customer: "test",
					Type:     Customer,
					Seats:    10,
				},
			},
			usedSeats: 12,
			expWarnings: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: fmt.Sprintf(licenseSeatsExceededGraceTemplate, 12, 10),
				},
			},
			err: nil,
		},
		{
			name: "testSeatsCustomerFail",
			license: License{
				Terms: Terms{
					Customer: "test",
					Type:     Customer,
					Seats:    4,
				},
			},
			usedSeats:   6,
			expWarnings: nil,
			err:         errors.NewFriendlyError(licenseSeatsExceededTemplate, 6, 4),
		},
		{
			name: "testSeatsTrialOk",
			license: License{
				Terms: Terms{
					Customer: "test",
					Type:     Trial,
				},
			},
			usedSeats:   0,
			expWarnings: nil,
			err:         nil,
		},
		{
			name: "testSeatsTrialWarn",
			license: License{
				Terms: Terms{
					Customer: "test",
					Type:     Trial,
				},
			},
			usedSeats: 2,
			expWarnings: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: fmt.Sprintf(licenseSeatsExceededGraceTemplate, 2, 1),
				},
			},
			err: nil,
		},
		{
			name: "testSeatsTrialFail",
			license: License{
				Terms: Terms{
					Customer: "test",
					Type:     Trial,
				},
			},
			usedSeats:   3,
			expWarnings: nil,
			err:         errors.NewFriendlyError(licenseSeatsExceededTemplate, 3, 1),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			warn, e := test.license.CheckSeats(test.usedSeats)
			assert.Equal(t, test.expWarnings, warn)
			assert.Equal(t, test.err, e)
		})
	}
}

func TestCheckExpiration(t *testing.T) {
	expiryTime := time.Unix(1136239445, 0).UTC()
	expiryTimeStr := expiryTime.Format(time.RFC1123)

	tests := []struct {
		name        string
		license     License
		time        time.Time
		expWarnings []*messages.Message
		err         error
	}{
		{
			name: "testExpiryCustomerOk",
			license: License{
				Terms: Terms{
					Customer:   "test",
					Type:       Customer,
					Seats:      10,
					ExpiryTime: expiryTime,
				},
			},
			time:        expiryTime.Add(-24 * 365 * time.Hour),
			expWarnings: nil,
			err:         nil,
		},
		{
			name: "testExpiryCustomerWarn",
			license: License{
				Terms: Terms{
					Customer:   "test",
					Type:       Customer,
					Seats:      10,
					ExpiryTime: expiryTime,
				},
			},
			time: expiryTime.Add(-24 * 10 * time.Hour),
			expWarnings: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: fmt.Sprintf(licenseCustomerWarnTemplate, expiryTimeStr),
				},
			},
			err: nil,
		},
		{
			name: "testExpiryCustomerWarn2",
			license: License{
				Terms: Terms{
					Customer:   "test",
					Type:       Customer,
					Seats:      10,
					ExpiryTime: expiryTime,
				},
			},
			time: expiryTime.Add(-24 * 1 * time.Hour),
			expWarnings: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: fmt.Sprintf(licenseCustomerWarnTemplate, expiryTimeStr),
				},
			},
			err: nil,
		},
		{
			name: "testExpiryCustomerGrace",
			license: License{
				Terms: Terms{
					Customer:   "test",
					Type:       Customer,
					Seats:      10,
					ExpiryTime: expiryTime,
				},
			},
			time: expiryTime.Add(24 * 1 * time.Hour),
			expWarnings: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: fmt.Sprintf(licenseCustomerGraceTemplate, expiryTimeStr),
				},
			},
			err: nil,
		},
		{
			name: "testExpiryCustomerGrace2",
			license: License{
				Terms: Terms{
					Customer:   "test",
					Type:       Customer,
					Seats:      10,
					ExpiryTime: expiryTime,
				},
			},
			time: expiryTime.Add(24 * 13 * time.Hour),
			expWarnings: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: fmt.Sprintf(licenseCustomerGraceTemplate, expiryTimeStr),
				},
			},
			err: nil,
		},
		{
			name: "testExpiryCustomerFail",
			license: License{
				Terms: Terms{
					Customer:   "test",
					Type:       Customer,
					Seats:      10,
					ExpiryTime: expiryTime,
				},
			},
			time:        expiryTime.Add(24 * 15 * time.Hour),
			expWarnings: nil,
			err:         errors.NewFriendlyError(licenseCustomerExpiredTemplate, expiryTimeStr),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			getCurrentTime = func() time.Time {
				return test.time
			}
			warn, e := test.license.CheckExpiration()
			assert.Equal(t, test.expWarnings, warn)
			assert.Equal(t, test.err, e)
		})
	}
}
