/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package license

import (
	"fmt"
	"time"

	"github.com/unidoc/unidoc/common"
)

const (
	LicenseTierUnlicensed = "unlicensed"
	LicenseTierCommunity  = "community"
	LicenseTierIndividual = "individual"
	LicenseTierBusiness   = "business"
)

// Make sure all time is at least after this for sanity check.
var testTime = time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)

type LicenseKey struct {
	LicenseId    string    `json:"license_id"`
	CustomerId   string    `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	Tier         string    `json:"tier"`
	CreatedAt    time.Time `json:"-"`
	CreatedAtInt int64     `json:"created_at"`
	CreatedBy    string    `json:"created_by"`
	CreatorName  string    `json:"creator_name"`
	CreatorEmail string    `json:"creator_email"`
}

func (this *LicenseKey) Validate() error {
	if len(this.LicenseId) < 10 {
		return fmt.Errorf("Invalid license: License Id")
	}

	if len(this.CustomerId) < 10 {
		return fmt.Errorf("Invalid license: Customer Id")
	}

	if len(this.CustomerName) < 1 {
		return fmt.Errorf("Invalid license: Customer Name")
	}

	if testTime.After(this.CreatedAt) {
		return fmt.Errorf("Invalid license: Created At is invalid")
	}

	if len(this.CreatorName) < 1 {
		return fmt.Errorf("Invalid license: Creator name")
	}

	if len(this.CreatorEmail) < 1 {
		return fmt.Errorf("Invalid license: Creator email")
	}

	return nil
}

func (this *LicenseKey) TypeToString() string {
	if this.Tier == LicenseTierUnlicensed {
		return "Unlicensed"
	}

	if this.Tier == LicenseTierCommunity {
		return "AGPLv3 Open Source Community License"
	}

	if this.Tier == LicenseTierIndividual || this.Tier == "indie" {
		return "Commercial License - Individual"
	}

	return "Commercial License - Business"
}

func (this *LicenseKey) ToString() string {
	str := fmt.Sprintf("License Id: %s\n", this.LicenseId)
	str += fmt.Sprintf("Customer Id: %s\n", this.CustomerId)
	str += fmt.Sprintf("Customer Name: %s\n", this.CustomerName)
	str += fmt.Sprintf("Tier: %s\n", this.Tier)
	str += fmt.Sprintf("Created At: %s\n", common.UtcTimeFormat(this.CreatedAt))
	str += fmt.Sprintf("Creator: %s <%s>\n", this.CreatorName, this.CreatorEmail)
	return str
}

func (lk *LicenseKey) IsLicensed() bool {
	return lk.Tier != LicenseTierUnlicensed
}

func MakeUnlicensedKey() *LicenseKey {
	lk := LicenseKey{}
	lk.CustomerName = "Unlicensed"
	lk.Tier = LicenseTierUnlicensed
	lk.CreatedAt = time.Now().UTC()
	lk.CreatedAtInt = lk.CreatedAt.Unix()
	return &lk
}
