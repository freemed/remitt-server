// Package model: X12 remittance DTO JSON contract (x12dto.go).
//
// # Scope
//
// The x12dto structs are the interchange-neutral representation handed between
// the parser, the render plugins and any caller that serialises a remittance
// advice. They are pure data: no database, no I/O. These tests pin the exact
// JSON document each type produces, because that document is the only published
// contract for external consumers.
//
// # What is pinned
//
//   - Field names (snake_case, e.g. billing_provider_tax_id) and the fact that
//     the DTOs use NO omitempty except on the Payer/Payee pointers and the
//     X12Address pointers they contain: zero numbers and empty strings are
//     always present in the output, so consumers can rely on the keys.
//   - Nullable things are expressed as JSON null (a nil slice) rather than an
//     omitted key or an empty array, while a non-nil empty slice marshals as [].
//   - Decoding is tolerant of unknown keys and strict about types.
//   - Nested values survive a marshal/unmarshal round trip.
package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestX12DTOZeroValueJSON(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "X12Address",
			in:   X12Address{},
			want: `{"addr1":"","addr2":"","city":"","state":"","zip":""}`,
		},
		{
			name: "X12Amount",
			in:   X12Amount{},
			want: `{"amount":0,"qualifier":""}`,
		},
		{
			name: "X12Identification",
			in:   X12Identification{},
			want: `{"qualifier":"","id":""}`,
		},
		{
			name: "X12Insured",
			in:   X12Insured{},
			want: `{"last_name":"","first_name":"","middle_name":"","suffix":"","identification":null}`,
		},
		{
			name: "X12Patient",
			in:   X12Patient{},
			want: `{"last_name":"","first_name":"","middle_name":"","suffix":"","identification":null}`,
		},
		{
			name: "X12Payee_without_address",
			in:   X12Payee{},
			want: `{"name":"","identification":null}`,
		},
		{
			name: "X12Payer_without_address",
			in:   X12Payer{},
			want: `{"name":"","identification":null}`,
		},
		{
			name: "X12ClaimAdjustment",
			in:   X12ClaimAdjustment{},
			want: `{"group_code":"","reason_code":"","amount":0,"quantity":0}`,
		},
		{
			name: "X12ClaimInformation",
			in:   X12ClaimInformation{},
			want: `{"claim_status":"","claim_number":"","patient_control_number":"","billing_provider_last_name":"","billing_provider_first_name":"","billing_provider_middle_name":"","billing_provider_tax_id":"","billing_provider_npi":"","service_date_from":"","service_date_to":"","claim_charge_amount":0,"patient_responsibility_amount":0,"claim_payment_amount":0,"patient_paid_amount":0,"claim_adjustments":null}`,
		},
		{
			name: "X12ClaimPayment",
			in:   X12ClaimPayment{},
			want: `{"payer_claim_control_number":"","patient_control_number":"","claim_number":"","claim_status":"","check_number":"","check_date":"","billed_amount":0,"claim_charge_amount":0,"paid_amount":0,"patient_responsibility_amount":0,"claim_adjustments":null}`,
		},
		{
			name: "X12ProviderClaimGroup",
			in:   X12ProviderClaimGroup{},
			want: `{"provider_tax_id":"","provider_npi":"","claims":null}`,
		},
		{
			name: "X12Remittance",
			in:   X12Remittance{},
			want: `{"check_number":"","check_date":"","check_amount":0,"credit_amount":0,"provider_claim_groups":null}`,
		},
		{
			name: "X12TransactionSet",
			in:   X12TransactionSet{},
			want: `{"trace_number":"","remittances":null}`,
		},
		{
			name: "X12FunctionalAck",
			in:   X12FunctionalAck{},
			want: `{"ack_code":"","errors":null}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("zero value JSON:\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestX12DTOPopulatedJSON(t *testing.T) {
	t.Run("address_and_identification", func(t *testing.T) {
		addr := X12Address{Addr1: "1 Main St", Addr2: "Apt 2", City: "Hartford", State: "CT", Zip: "06103"}
		payee := X12Payee{
			Name:           "TEST MEDICAL GROUP",
			Identification: []X12Identification{{Qualifier: "XX", ID: "1234567890"}, {Qualifier: "PI", ID: "NPI1"}},
			Address:        &addr,
		}
		got, err := json.Marshal(payee)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"name":"TEST MEDICAL GROUP","identification":[{"qualifier":"XX","id":"1234567890"},{"qualifier":"PI","id":"NPI1"}],"address":{"addr1":"1 Main St","addr2":"Apt 2","city":"Hartford","state":"CT","zip":"06103"}}`
		if string(got) != want {
			t.Errorf("payee JSON:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("full_transaction_set", func(t *testing.T) {
		ts := X12TransactionSet{
			TraceNumber: "TRACE-001",
			Payer:       &X12Payer{Name: "TEST PAYER"},
			Remittances: []X12Remittance{{
				CheckNumber:  "CHK1",
				CheckDate:    "20240810",
				CheckAmount:  1250,
				CreditAmount: 0.5,
				ProviderClaimGroups: []X12ProviderClaimGroup{{
					ProviderTaxID: "1234567890",
					ProviderNPI:   "NPI9",
					Claims: []X12ClaimPayment{{
						ClaimNumber:      "CLAIM001",
						PaidAmount:       400,
						ClaimAdjustments: []X12ClaimAdjustment{{GroupCode: "CO", ReasonCode: "45", Amount: 100, Quantity: 1}},
					}},
				}},
			}},
		}
		got, err := json.Marshal(ts)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"trace_number":"TRACE-001","payer":{"name":"TEST PAYER","identification":null},"remittances":[{"check_number":"CHK1","check_date":"20240810","check_amount":1250,"credit_amount":0.5,"provider_claim_groups":[{"provider_tax_id":"1234567890","provider_npi":"NPI9","claims":[{"payer_claim_control_number":"","patient_control_number":"","claim_number":"CLAIM001","claim_status":"","check_number":"","check_date":"","billed_amount":0,"claim_charge_amount":0,"paid_amount":400,"patient_responsibility_amount":0,"claim_adjustments":[{"group_code":"CO","reason_code":"45","amount":100,"quantity":1}]}]}]}]}`
		if string(got) != want {
			t.Errorf("transaction set JSON:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("payee_with_empty_but_non_nil_address", func(t *testing.T) {
		// A pointer keeps omitempty meaningful: an empty struct behind a pointer
		// is still emitted, only a nil pointer disappears.
		got, err := json.Marshal(X12Payee{Name: "P", Address: &X12Address{}})
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !strings.Contains(string(got), `"address":{"addr1":"","addr2":"","city":"","state":"","zip":""}`) {
			t.Errorf("empty address pointer should be emitted, got: %s", got)
		}
	})

	t.Run("empty_but_non_nil_slices", func(t *testing.T) {
		got, err := json.Marshal(X12FunctionalAck{Errors: []string{}})
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if string(got) != `{"ack_code":"","errors":[]}` {
			t.Errorf("JSON = %s, want an empty array rather than null", got)
		}

		got, err = json.Marshal(X12Remittance{ProviderClaimGroups: []X12ProviderClaimGroup{}})
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !strings.Contains(string(got), `"provider_claim_groups":[]`) {
			t.Errorf("JSON = %s, want an empty array rather than null", got)
		}
	})
}

func TestX12DTOTagContract(t *testing.T) {
	// Field names alone are pinned above; this checks the tags behind them so a
	// rename is reported as the tag that changed, and confirms which fields
	// carry omitempty.
	cases := []struct {
		name       string
		typ        reflect.Type
		wantTags   map[string]string
		omitEmpty  []string
		wantFields int
	}{
		{
			name: "X12Address",
			typ:  reflect.TypeOf(X12Address{}),
			wantTags: map[string]string{
				"Addr1": "addr1", "Addr2": "addr2", "City": "city", "State": "state", "Zip": "zip",
			},
			wantFields: 5,
		},
		{
			name: "X12Payee",
			typ:  reflect.TypeOf(X12Payee{}),
			wantTags: map[string]string{
				"Name": "name", "Identification": "identification", "Address": "address,omitempty",
			},
			omitEmpty:  []string{"Address"},
			wantFields: 3,
		},
		{
			name: "X12Payer",
			typ:  reflect.TypeOf(X12Payer{}),
			wantTags: map[string]string{
				"Name": "name", "Identification": "identification", "Address": "address,omitempty",
			},
			omitEmpty:  []string{"Address"},
			wantFields: 3,
		},
		{
			name: "X12TransactionSet",
			typ:  reflect.TypeOf(X12TransactionSet{}),
			wantTags: map[string]string{
				"TraceNumber": "trace_number", "Payer": "payer,omitempty", "Payee": "payee,omitempty", "Remittances": "remittances",
			},
			omitEmpty:  []string{"Payer", "Payee"},
			wantFields: 4,
		},
		{
			name: "X12Remittance",
			typ:  reflect.TypeOf(X12Remittance{}),
			wantTags: map[string]string{
				"Payer": "payer,omitempty", "Payee": "payee,omitempty", "CheckNumber": "check_number",
				"CheckDate": "check_date", "CheckAmount": "check_amount", "CreditAmount": "credit_amount",
				"ProviderClaimGroups": "provider_claim_groups",
			},
			omitEmpty:  []string{"Payer", "Payee"},
			wantFields: 7,
		},
		{
			name: "X12FunctionalAck",
			typ:  reflect.TypeOf(X12FunctionalAck{}),
			wantTags: map[string]string{
				"AckCode": "ack_code", "Errors": "errors",
			},
			wantFields: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.typ.NumField() != tc.wantFields {
				t.Errorf("%s has %d fields, want %d", tc.name, tc.typ.NumField(), tc.wantFields)
			}
			omit := map[string]bool{}
			for _, f := range tc.omitEmpty {
				omit[f] = true
			}
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i)
				want, ok := tc.wantTags[f.Name]
				if !ok {
					t.Errorf("unexpected field %s.%s", tc.name, f.Name)
					continue
				}
				if got := f.Tag.Get("json"); got != want {
					t.Errorf("%s.%s json tag = %q, want %q", tc.name, f.Name, got, want)
				}
				hasOmit := strings.Contains(want, ",omitempty")
				if omit[f.Name] != hasOmit {
					t.Errorf("%s.%s omitempty mismatch: tag %q, expectation omitempty=%v", tc.name, f.Name, want, omit[f.Name])
				}
			}
		})
	}
}

func TestX12DTODecoding(t *testing.T) {
	t.Run("unknown_fields_are_ignored", func(t *testing.T) {
		var r X12Remittance
		err := json.Unmarshal([]byte(`{"check_number":"CHK1","future_field":{"nested":[1,2]},"provider_claim_groups":[]}`), &r)
		if err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if r.CheckNumber != "CHK1" {
			t.Errorf("check_number = %q, want CHK1", r.CheckNumber)
		}
		if r.ProviderClaimGroups == nil || len(r.ProviderClaimGroups) != 0 {
			t.Errorf("provider_claim_groups = %#v, want an empty non-nil slice", r.ProviderClaimGroups)
		}
	})

	t.Run("wrong_types_are_rejected", func(t *testing.T) {
		cases := []struct {
			name string
			doc  string
			want string
		}{
			{name: "string_for_amount",
				doc:  `{"check_amount":"1250.00"}`,
				want: "json: cannot unmarshal string into Go struct field X12Remittance.check_amount of type float64"},
			{name: "object_for_slice",
				doc:  `{"provider_claim_groups":{}}`,
				want: "cannot unmarshal object into Go struct field X12Remittance.provider_claim_groups of type []model.X12ProviderClaimGroup"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var r X12Remittance
				err := json.Unmarshal([]byte(tc.doc), &r)
				if err == nil {
					t.Fatalf("expected an error for %s", tc.doc)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %v, want it to contain %q", err, tc.want)
				}
			})
		}
	})

	t.Run("null_clears_pointers_and_slices", func(t *testing.T) {
		ts := X12TransactionSet{
			Payer:       &X12Payer{Name: "P"},
			Remittances: []X12Remittance{{CheckNumber: "C"}},
		}
		if err := json.Unmarshal([]byte(`{"payer":null,"remittances":null}`), &ts); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if ts.Payer != nil {
			t.Errorf("payer = %+v, want nil", ts.Payer)
		}
		if ts.Remittances != nil {
			t.Errorf("remittances = %+v, want nil", ts.Remittances)
		}
	})

	t.Run("numbers_keep_full_precision", func(t *testing.T) {
		var r X12Remittance
		if err := json.Unmarshal([]byte(`{"check_amount":1234567.89,"credit_amount":-0.01}`), &r); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if r.CheckAmount != 1234567.89 {
			t.Errorf("check_amount = %v, want 1234567.89", r.CheckAmount)
		}
		if r.CreditAmount != -0.01 {
			t.Errorf("credit_amount = %v, want -0.01", r.CreditAmount)
		}
	})
}

func TestX12DTORoundTrip(t *testing.T) {
	original := X12TransactionSet{
		TraceNumber: "TRACE-001",
		Payer: &X12Payer{
			Name:           "TEST PAYER",
			Identification: []X12Identification{{Qualifier: "PI", ID: "PAYER1"}},
			Address:        &X12Address{City: "Hartford", State: "CT"},
		},
		Payee: &X12Payee{Name: "TEST PAYEE"},
		Remittances: []X12Remittance{{
			CheckNumber: "CHK1",
			CheckDate:   "20240810",
			CheckAmount: 1250.00,
			ProviderClaimGroups: []X12ProviderClaimGroup{{
				ProviderTaxID: "1234567890",
				Claims: []X12ClaimPayment{{
					ClaimNumber:      "CLAIM001",
					ClaimStatus:      "1",
					PaidAmount:       400,
					BilledAmount:     500,
					ClaimAdjustments: []X12ClaimAdjustment{{GroupCode: "CO", ReasonCode: "45", Amount: 100}},
				}},
			}},
		}},
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var back X12TransactionSet
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, back) {
		t.Errorf("round trip changed the document:\n got: %s\nwant: %#v", b, original)
	}
}

func TestX12ClaimInformationAndInsuredShareShape(t *testing.T) {
	// X12Insured and X12Patient are declared separately but identically; the
	// tests pin that they stay interchangeable (a caller may pass either).
	insured := reflect.TypeOf(X12Insured{})
	patient := reflect.TypeOf(X12Patient{})
	if insured.NumField() != patient.NumField() {
		t.Fatalf("field counts differ: %d vs %d", insured.NumField(), patient.NumField())
	}
	for i := 0; i < insured.NumField(); i++ {
		fi, fp := insured.Field(i), patient.Field(i)
		if fi.Name != fp.Name || fi.Type != fp.Type || fi.Tag != fp.Tag {
			t.Errorf("field %d differs: %v vs %v", i, fi, fp)
		}
	}
}
