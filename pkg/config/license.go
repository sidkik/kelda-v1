package config

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"golang.org/x/crypto/ed25519"

	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/proto/messages"
)

const (
	// licenseVersionMismatchTemplate is a friendly error template shown to the
	// user when the license version doesn't match the supported version.
	licenseVersionMismatchTemplate = "The supported license " +
		"version is %q while the existing license version is %q. " +
		"Please contact your Kelda administrator to obtain a new license."

	// licenseSeatsExceededGraceThreshold is the proportion of seats above the purchased limit the customer may use
	// before the application hard-fails. Defaults to 25%
	licenseSeatsExceededGraceThreshold = float64(1.25)

	// licenseSeatsExceededTemplate is the error shown when the customer has exceeded the number of seats
	// they've purchased on their license.
	licenseSeatsExceededTemplate = "Application seats in use exceeds grace threshold: " +
		"%d seats used of %d purchased.\n" +
		"Please contact your Kelda administrator to expand your license."

	// licenseSeatsExceededGraceTemplate is the warning shown to the customer when they've exceeded
	// the number of seats purchased, but we give them some buffer before hard-failing so they can
	// continue to work while getting a new license
	licenseSeatsExceededGraceTemplate = "Application seats in use exceeds licensed seats: " +
		"%d seats used of %d purchased.\n" +
		"Entering grace period. Please contact your Kelda administrator to expand your license."

	// licenseCustomerWarningPeriod is the amount of time before license expiry that the
	// customer starts getting warned about renewing their Kelda license. Defaults to 30 days.
	licenseCustomerWarningPeriod = time.Duration(24*30) * time.Hour

	// licenseCustomerGracePeriod is the amount of time after license expiry that the customer
	// has to get a new license before the application hard-fails. Defaults to 14 days.
	licenseCustomerGracePeriod = time.Duration(24*14) * time.Hour

	// licenseCustomerExpiredTemplate is the error shown to the customer once their license and the expiry grace
	// period are both exhausted.
	licenseCustomerExpiredTemplate = "Your Kelda license expired on %s.\n" +
		"Please contact your Kelda administrator to renew your license."

	// licenseCustomerGracePeriodTemplate is the warning shown to the customer when the license has expired
	// but the expiry grace period is still active.
	licenseCustomerGraceTemplate = "Your Kelda license expired on %s. " +
		"You may continue to use the application during the grace period.\n" +
		"Please contact your Kelda administrator to renew your license."

	// licenseCustomerWarnTemplate is the warning shown to the customer when the license is approaching
	// expiration.
	licenseCustomerWarnTemplate = "Approaching license expiry time: your Kelda license expires on %s."

	// timeFormat is the chosen representation format for displaying timestamps. RFC1123 is relatively friendly.
	timeFormat = time.RFC1123

	// Customer is a LicenseType indicating the license is for a signed-and-paid customer
	Customer LicenseType = 0

	// Trial is a LicenseType indicating a license for a client desiring a free trial
	Trial LicenseType = 1
)

var (
	// licenseVerificationPublicKey is an ed25519 public key used to verify licenses. The corresponding
	// private key is in make-license/main.go.
	licenseVerificationPublicKey = mustDecodePublicKey("912d3747e279c0c65a50080cced564b1224cebc5c7975a7967f99ecdff961990")

	// SupportedLicenseVersion is the supported version of the license
	// configuration.
	SupportedLicenseVersion = "v1alpha2"

	// For mocking in the tests
	getCurrentTime = time.Now
)

// License contains the configuration required to grant a user access to use
// Kelda. Access is gated on the number of seats purchased and an expiry time.
type License struct {
	// The version of the license format.
	Version string

	// The terms of the license.
	Terms Terms

	// Whether the license was properly signed.
	Signed bool
}

// Terms contains the actionable terms of the license
type Terms struct {
	Customer   string      // the name of the customer
	Type       LicenseType // what type of license it is
	Seats      int         // how many seats the customer purchased
	ExpiryTime time.Time   // when the license expires
}

// LicenseType is the type of the license, currently just "trial" or "customer"
type LicenseType int

// signedLicenseWireFormat is the JSON structure used for serializing Licenses to disk.
type signedLicenseWireFormat struct {
	// The JSON encoding for the license, represented by `licenseWireFormat`.
	License []byte

	// The contents of the `License` field signed by the licensing private key.
	Signature []byte

	// The version of the license format.
	Version string
}

// licenseWireFormat is the JSON structure used for the License field of
// `signedLicenseWireFormat`.
type licenseWireFormat struct {
	Terms Terms
}

// ParseLicense parses the license with the given contents. It expects licenses
// generated by License.Marshal.
func ParseLicense(licenseStr string) (License, error) {
	licenseJSON, err := base64.StdEncoding.DecodeString(licenseStr)
	if err != nil {
		return License{}, errors.WithContext(err, "base64 decode")
	}

	var signedLicense signedLicenseWireFormat
	if err := json.Unmarshal(licenseJSON, &signedLicense); err != nil {
		return License{}, errors.WithContext(err, "parse outer license")
	}

	if signedLicense.Version != SupportedLicenseVersion {
		return License{}, errors.NewFriendlyError(licenseVersionMismatchTemplate,
			SupportedLicenseVersion, signedLicense.Version)
	}

	var license licenseWireFormat
	if err := json.Unmarshal(signedLicense.License, &license); err != nil {
		return License{}, errors.WithContext(err, "parse inner license")
	}

	return License{
		Version: signedLicense.Version,
		Terms:   license.Terms,
		Signed:  ed25519.Verify(licenseVerificationPublicKey, signedLicense.License, signedLicense.Signature),
	}, nil
}

func (license License) Marshal(signingKey ed25519.PrivateKey) (string, error) {
	licenseBytes, err := json.Marshal(licenseWireFormat{Terms: license.Terms})
	if err != nil {
		return "", errors.WithContext(err, "marshal terms")
	}

	var signature []byte
	if signingKey != nil {
		signature = ed25519.Sign(signingKey, licenseBytes)
	}

	signedLicenseBytes, err := json.Marshal(signedLicenseWireFormat{
		License:   licenseBytes,
		Signature: signature,
		Version:   SupportedLicenseVersion,
	})
	if err != nil {
		return "", errors.WithContext(err, "marshal license")
	}

	return base64.StdEncoding.EncodeToString(signedLicenseBytes), nil
}

// CheckSeats checks the customer is using a permissible number of seats.
func (license License) CheckSeats(usedSeats int) ([]*messages.Message, error) {
	switch license.Terms.Type {
	case Trial:
		// A trial license can have at most one user.
		return checkLicenseSeats(1, usedSeats)
	case Customer:
		return checkLicenseSeats(license.Terms.Seats, usedSeats)
	default:
		return nil, errors.New("unrecognized license type")
	}
}

// CheckExpiration checks that the license has not expired.
func (license License) CheckExpiration() ([]*messages.Message, error) {
	switch license.Terms.Type {
	case Trial:
		// A trial license never expires.
		return nil, nil
	case Customer:
		// A customer license gets both expiry and seat-based checks
		return checkLicenseExpiry(
			license.Terms,
			licenseCustomerWarningPeriod, licenseCustomerGracePeriod,
			licenseCustomerExpiredTemplate, licenseCustomerWarnTemplate, licenseCustomerGraceTemplate,
		)
	default:
		return nil, errors.New("unrecognized license type")
	}
}

func checkLicenseSeats(maxSeats, usedSeats int) (warnings []*messages.Message, err error) {
	allowedSeats := int(math.Ceil(float64(maxSeats) * licenseSeatsExceededGraceThreshold))
	switch {
	// Fail if they are out of seats + grace amount.
	case usedSeats > allowedSeats:
		err = errors.NewFriendlyError(licenseSeatsExceededTemplate, usedSeats, maxSeats)
	// Warn if they have exceeded the seat limit but are within the threshold.
	case usedSeats > maxSeats:
		warnings = append(warnings, &messages.Message{
			Type: messages.Message_WARNING,
			Text: fmt.Sprintf(licenseSeatsExceededGraceTemplate, usedSeats, maxSeats),
		})
	}

	return warnings, err
}

// checkLicenseExpiry checks that the time-based terms of the license are met.
// We allow a warning and grace period on the license's configured expiry time,
// which send alerts back to the client. It fails after the grace period is up.
func checkLicenseExpiry(terms Terms, warningPeriod, gracePeriod time.Duration,
	expiredTemplate, warnTemplate, graceTemplate string) (
	warnings []*messages.Message, err error) {

	currentTime := getCurrentTime()
	licenseExpiryTimeStr := terms.ExpiryTime.Format(timeFormat)
	expirationWithGracePeriod := terms.ExpiryTime.Add(gracePeriod)
	warningDate := terms.ExpiryTime.Add(-warningPeriod)

	// Current time is after the license + grace period.
	hasExpired := currentTime.After(expirationWithGracePeriod)
	// It's after license expiry, but before the grace period is up.
	inGracePeriod := currentTime.After(terms.ExpiryTime) && currentTime.Before(expirationWithGracePeriod)
	// It's before license expiry, but after the warning period starts.
	inWarnPeriod := currentTime.Before(terms.ExpiryTime) && currentTime.After(warningDate)

	switch {
	case hasExpired:
		err = errors.NewFriendlyError(expiredTemplate, licenseExpiryTimeStr)
	case inGracePeriod:
		warnings = append(warnings, &messages.Message{
			Type: messages.Message_WARNING,
			Text: fmt.Sprintf(graceTemplate, licenseExpiryTimeStr),
		})
	case inWarnPeriod:
		warnings = append(warnings, &messages.Message{
			Type: messages.Message_WARNING,
			Text: fmt.Sprintf(warnTemplate, licenseExpiryTimeStr),
		})
	}

	return warnings, err
}

func mustDecodePublicKey(keyHex string) ed25519.PublicKey {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		panic(err)
	}
	return ed25519.PublicKey(keyBytes)
}
