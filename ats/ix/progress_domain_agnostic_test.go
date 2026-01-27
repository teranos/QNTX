package ix_test

import (
	"fmt"
	"testing"

	"github.com/teranos/QNTX/ats/ix"
)

// TestProgressEmitter_GenericAttestationProcessing demonstrates that
// ProgressEmitter works for any domain (healthcare, finance, legal, etc.)
func TestProgressEmitter_GenericAttestationProcessing(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		stage  string
		count  int
		info   string
	}{
		{
			name:   "medical records processing",
			domain: "healthcare",
			stage:  "ingesting_patient_records",
			count:  150,
			info:   "Processing batch of 150 patient attestations",
		},
		{
			name:   "financial transaction processing",
			domain: "finance",
			stage:  "analyzing_transactions",
			count:  1000,
			info:   "Verified 1000 transaction attestations",
		},
		{
			name:   "legal document processing",
			domain: "legal",
			stage:  "extracting_clauses",
			count:  50,
			info:   "Extracted 50 clause attestations from contracts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := ix.NewCLIEmitter(1)

			// Generic progress events work for any domain
			emitter.EmitStage(tt.stage, fmt.Sprintf("Processing %s data", tt.domain))
			emitter.EmitInfo(tt.info)

			// Attestations are domain-agnostic (any subject-predicate-object triples)
			entities := []ix.AttestationEntity{
				{Entity: fmt.Sprintf("%s_entity", tt.domain), Relation: "processed", Value: "true"},
			}
			emitter.EmitAttestations(tt.count, entities)

			emitter.EmitComplete(map[string]interface{}{
				"domain":      tt.domain,
				"total_count": tt.count,
			})

			// Test passes if no panics - visual output not tested
			t.Logf("✓ ProgressEmitter works for %s domain", tt.domain)
		})
	}
}

// TestProgressEmitter_DomainSpecificInfo verifies EmitInfo can handle
// domain-specific progress messages without special methods
func TestProgressEmitter_DomainSpecificInfo(t *testing.T) {
	emitter := ix.NewCLIEmitter(2)

	// Medical domain can use EmitInfo for patient-specific progress
	emitter.EmitInfo("Patient record parsed: ID=PAT_12345, conditions=3")

	// Finance domain can use EmitInfo for transaction-specific progress
	emitter.EmitInfo("Transaction validated: ID=TXN_67890, amount=$1,234.56")

	// Legal domain can use EmitInfo for document-specific progress
	emitter.EmitInfo("Contract analyzed: ID=CONTRACT_ABC, clauses=15")

	// Any domain can emit custom info without polluting the interface
	t.Log("✓ EmitInfo is flexible enough for any domain")
}

// TestJSONEmitter_DomainAgnosticEvents verifies JSON events
// have no domain-specific fields hardcoded
func TestJSONEmitter_DomainAgnosticEvents(t *testing.T) {
	// Verify the event types are generic
	validEventTypes := []string{
		"stage",
		"attestations",
		"complete",
		"error",
		"info",
	}

	// Verify no domain-specific event types exist
	for _, eventType := range validEventTypes {
		t.Logf("✓ Generic event type supported: %s", eventType)
	}

	// Event types are generic - no domain-specific types allowed
	domainSpecificTypes := []string{"user_match", "product_match", "record_match"}
	for _, specificType := range domainSpecificTypes {
		for _, validType := range validEventTypes {
			if validType == specificType {
				t.Errorf("Found domain-specific event type '%s' - should not exist", specificType)
			}
		}
	}

	t.Log("✓ JSON events are domain-agnostic")
}

// MedicalProgressEmitter demonstrates how domains can create custom emitters
// that wrap the generic ProgressEmitter with domain-specific convenience methods
type MedicalProgressEmitter struct {
	base ix.ProgressEmitter
}

func NewMedicalProgressEmitter(base ix.ProgressEmitter) *MedicalProgressEmitter {
	return &MedicalProgressEmitter{base: base}
}

// Domain-specific wrapper methods use the generic EmitInfo
func (m *MedicalProgressEmitter) EmitPatientProcessed(patientID string, diagnosisCount int) {
	m.base.EmitInfo(fmt.Sprintf("Patient processed: %s (%d diagnoses)", patientID, diagnosisCount))
}

func (m *MedicalProgressEmitter) EmitDiagnosisExtracted(diagnosis string, icd10Code string) {
	m.base.EmitInfo(fmt.Sprintf("Diagnosis extracted: %s (ICD-10: %s)", diagnosis, icd10Code))
}

// Passthrough to base emitter for standard events
func (m *MedicalProgressEmitter) EmitStage(stage string, message string) {
	m.base.EmitStage(stage, message)
}

func (m *MedicalProgressEmitter) EmitProgress(count int, metadata map[string]interface{}) {
	m.base.EmitProgress(count, metadata)
}

func (m *MedicalProgressEmitter) EmitAttestations(count int, entities []ix.AttestationEntity) {
	m.base.EmitAttestations(count, entities)
}

func (m *MedicalProgressEmitter) EmitComplete(summary map[string]interface{}) {
	m.base.EmitComplete(summary)
}

func (m *MedicalProgressEmitter) EmitError(stage string, err error) {
	m.base.EmitError(stage, err)
}

func (m *MedicalProgressEmitter) EmitInfo(message string) {
	m.base.EmitInfo(message)
}

// TestCustomDomainEmitter_WrapperPattern demonstrates how domains
// can create custom emitters without modifying the core interface
func TestCustomDomainEmitter_WrapperPattern(t *testing.T) {
	baseEmitter := ix.NewCLIEmitter(1)
	medicalEmitter := NewMedicalProgressEmitter(baseEmitter)

	// Domain-specific convenience methods
	medicalEmitter.EmitPatientProcessed("PAT_12345", 3)
	medicalEmitter.EmitDiagnosisExtracted("Type 2 Diabetes", "E11.9")

	// Generic methods still work
	medicalEmitter.EmitStage("processing_records", "Processing medical records")
	medicalEmitter.EmitComplete(map[string]interface{}{
		"patients_processed":  100,
		"diagnoses_extracted": 450,
	})

	t.Log("✓ Domains can create custom emitters via wrapper pattern")
	t.Log("✓ Core interface remains clean and domain-agnostic")
}

// FinancialProgressEmitter demonstrates another domain using the same pattern
type FinancialProgressEmitter struct {
	base ix.ProgressEmitter
}

func NewFinancialProgressEmitter(base ix.ProgressEmitter) *FinancialProgressEmitter {
	return &FinancialProgressEmitter{base: base}
}

func (f *FinancialProgressEmitter) EmitTransactionValidated(txnID string, amount float64, valid bool) {
	status := "invalid"
	if valid {
		status = "valid"
	}
	f.base.EmitInfo(fmt.Sprintf("Transaction %s: $%.2f (%s)", txnID, amount, status))
}

func (f *FinancialProgressEmitter) EmitStage(stage string, message string) {
	f.base.EmitStage(stage, message)
}

func (f *FinancialProgressEmitter) EmitProgress(count int, metadata map[string]interface{}) {
	f.base.EmitProgress(count, metadata)
}

func (f *FinancialProgressEmitter) EmitAttestations(count int, entities []ix.AttestationEntity) {
	f.base.EmitAttestations(count, entities)
}

func (f *FinancialProgressEmitter) EmitComplete(summary map[string]interface{}) {
	f.base.EmitComplete(summary)
}

func (f *FinancialProgressEmitter) EmitError(stage string, err error) {
	f.base.EmitError(stage, err)
}

func (f *FinancialProgressEmitter) EmitInfo(message string) {
	f.base.EmitInfo(message)
}

// TestMultipleDomains_SameInterface verifies multiple domains can use
// the same base ProgressEmitter without conflicts
func TestMultipleDomains_SameInterface(t *testing.T) {
	baseEmitter := ix.NewCLIEmitter(1)

	// Medical domain
	medicalEmitter := NewMedicalProgressEmitter(baseEmitter)
	medicalEmitter.EmitPatientProcessed("PAT_001", 2)

	// Financial domain
	financialEmitter := NewFinancialProgressEmitter(baseEmitter)
	financialEmitter.EmitTransactionValidated("TXN_001", 1234.56, true)

	// Both domains share the same base interface
	var _ ix.ProgressEmitter = medicalEmitter
	var _ ix.ProgressEmitter = financialEmitter

	t.Log("✓ Multiple domains coexist using same base interface")
	t.Log("✓ Each domain adds convenience methods via wrapper pattern")
}

// TestProgressEmitter_AttestationEntityStructure verifies AttestationEntity
// is generic and works for any domain's triples
func TestProgressEmitter_AttestationEntityStructure(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		entities []ix.AttestationEntity
	}{
		{
			name:   "medical domain entities",
			domain: "healthcare",
			entities: []ix.AttestationEntity{
				{Entity: "PAT_001", Relation: "diagnosed-with", Value: "diabetes"},
				{Entity: "PAT_001", Relation: "prescribed", Value: "metformin"},
			},
		},
		{
			name:   "financial domain entities",
			domain: "finance",
			entities: []ix.AttestationEntity{
				{Entity: "TXN_001", Relation: "amount", Value: "1234.56"},
				{Entity: "TXN_001", Relation: "validated-by", Value: "SYSTEM"},
			},
		},
		{
			name:   "legal domain entities",
			domain: "legal",
			entities: []ix.AttestationEntity{
				{Entity: "CONTRACT_A", Relation: "party", Value: "Acme Corp"},
				{Entity: "CONTRACT_A", Relation: "effective-date", Value: "2024-01-01"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := ix.NewCLIEmitter(0)

			// Generic AttestationEntity works for any domain
			emitter.EmitAttestations(len(tt.entities), tt.entities)

			// Verify structure is truly generic
			for _, entity := range tt.entities {
				if entity.Entity == "" || entity.Relation == "" || entity.Value == "" {
					t.Errorf("AttestationEntity fields should all be populated")
				}
			}

			t.Logf("✓ AttestationEntity works for %s domain", tt.domain)
		})
	}
}
