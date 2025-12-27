package async

import (
	"database/sql"
	"fmt"
)

// JobScanArgs holds all the variables needed for scanning a job from a database row.
// This follows the same pattern as contact/scan.go and organization/scan.go.
type JobScanArgs struct {
	HandlerName    sql.NullString
	Payload        sql.NullString
	PulseStateJSON sql.NullString
	ErrorMsg       sql.NullString
	ParentJobID    sql.NullString
	StartedAt      sql.NullTime
	CompletedAt    sql.NullTime
}

// GetJobScanArgs returns a JobScanArgs struct with all variables ready for scanning
func GetJobScanArgs() *JobScanArgs {
	return &JobScanArgs{}
}

// GetJobScanTargets returns a slice of interface{} pointers for the job and scan args,
// in the order expected by the standard job SELECT query
func GetJobScanTargets(job *Job, args *JobScanArgs) []interface{} {
	return []interface{}{
		&job.ID,
		&args.HandlerName,
		&job.Source,
		&job.Status,
		&job.Progress.Current,
		&job.Progress.Total,
		&job.CostEstimate,
		&job.CostActual,
		&args.PulseStateJSON,
		&args.ErrorMsg,
		&args.Payload,
		&args.ParentJobID,
		&job.RetryCount,
		&job.CreatedAt,
		&args.StartedAt,
		&args.CompletedAt,
		&job.UpdatedAt,
	}
}

// ProcessJobScanArgs processes the scanned arguments and populates the job struct.
// Returns an error if JSON unmarshaling fails.
func ProcessJobScanArgs(job *Job, args *JobScanArgs) error {
	// Set handler name
	if args.HandlerName.Valid {
		job.HandlerName = args.HandlerName.String
	}

	// Set payload (raw JSON, no unmarshaling needed)
	if args.Payload.Valid {
		job.Payload = []byte(args.Payload.String)
	}

	// Parse pulse state
	if args.PulseStateJSON.Valid {
		pulseState, err := UnmarshalPulseState(args.PulseStateJSON.String)
		if err != nil {
			return fmt.Errorf("failed to unmarshal pulse state for job %s: %w", job.ID, err)
		}
		job.PulseState = pulseState
	}

	// Set optional fields
	if args.ErrorMsg.Valid {
		job.Error = args.ErrorMsg.String
	}
	if args.ParentJobID.Valid {
		job.ParentJobID = args.ParentJobID.String
	}
	if args.StartedAt.Valid {
		job.StartedAt = &args.StartedAt.Time
	}
	if args.CompletedAt.Valid {
		job.CompletedAt = &args.CompletedAt.Time
	}

	return nil
}

// ScanJobFromRow scans a single job from a sql.Row
func ScanJobFromRow(row *sql.Row, job *Job) error {
	args := GetJobScanArgs()
	targets := GetJobScanTargets(job, args)

	if err := row.Scan(targets...); err != nil {
		return err
	}

	return ProcessJobScanArgs(job, args)
}

// ScanJobFromRows scans a single job from sql.Rows (for use in loops)
func ScanJobFromRows(rows *sql.Rows, job *Job) error {
	args := GetJobScanArgs()
	targets := GetJobScanTargets(job, args)

	if err := rows.Scan(targets...); err != nil {
		return err
	}

	return ProcessJobScanArgs(job, args)
}

// StandardJobSelectColumns returns the standard column list for job SELECT queries
func StandardJobSelectColumns() string {
	return `id, handler_name, source, status,
		progress_current, progress_total,
		cost_estimate, cost_actual,
		pulse_state, error, payload,
		parent_job_id, retry_count,
		created_at, started_at, completed_at, updated_at`
}
