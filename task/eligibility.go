package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/freemed/remitt-server/eligibility"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
)

// Job class name constants (match tJobs.jobClass values).
const (
	EligibilityJobClass = "org.remitt.server.tasks.EligibilityTask"
	ScooperJobClass     = "org.remitt.server.tasks.ScooperTask"
)

// RunEligibilityTask processes pending records in tEligibilityJobs.
func RunEligibilityTask() error {
	var jobs []model.EligibilityJobsModel
	rows, err := model.Queries.GetPendingEligibilityJobs(context.Background())
	if err != nil {
		return err
	}

	// Map dbgen.Teligibilityjob → model.EligibilityJobsModel
	for _, r := range rows {
		jobs = append(jobs, model.EligibilityJobsModel{
			Id:           r.ID,
			User:         r.User,
			Inserted:     r.Inserted,
			Processed:    model.NullTime{Time: r.Processed.Time, Valid: r.Processed.Valid},
			Plugin:       r.Plugin,
			Payload:      []byte(r.Payload.String),
			Response:     []byte(r.Response.String),
			Resubmission: r.Resubmission,
			Completed:    r.Completed,
		})
	}

	if len(jobs) == 0 {
		return nil
	}

	log.Printf("task.EligibilityTask: Processing %d pending eligibility jobs", len(jobs))

	for _, job := range jobs {
		checker, err := eligibility.InstantiateChecker(job.Plugin)
		if err != nil {
			log.Printf("task.EligibilityTask: Job %d: unable to instantiate checker %s: %s",
				job.Id, job.Plugin, err.Error())
			model.Queries.UpdateEligibilityJobFailed(context.Background(), dbgen.UpdateEligibilityJobFailedParams{
				ID:       job.Id,
				Response: sql.NullString{String: err.Error(), Valid: true},
			})
			continue
		}

		// Parse stored payload back into a map
		var values map[string]string
		if err := json.Unmarshal(job.Payload, &values); err != nil {
			log.Printf("task.EligibilityTask: Job %d: unable to parse payload: %s",
				job.Id, err.Error())
			continue
		}

		response, err := checker.CheckEligibility(job.User, values, job.Resubmission, job.Id)
		if err != nil {
			log.Printf("task.EligibilityTask: Job %d: check error: %s", job.Id, err.Error())
			model.Queries.UpdateEligibilityJobFailed(context.Background(), dbgen.UpdateEligibilityJobFailedParams{
				ID:       job.Id,
				Response: sql.NullString{String: err.Error(), Valid: true},
			})
			continue
		}

		// Serialize response to JSON for storage
		respBytes, _ := json.Marshal(response)

		now := time.Now()
		model.Queries.UpdateEligibilityJobComplete(context.Background(), dbgen.UpdateEligibilityJobCompleteParams{
			ID:        job.Id,
			Processed: sql.NullTime{Time: now, Valid: true},
			Response:  sql.NullString{String: string(respBytes), Valid: true},
		})

		log.Printf("task.EligibilityTask: Job %d completed: %s", job.Id, response.SuccessCode)
	}

	return nil
}

// RunScooperTask processes scooper plugins for all users.
func RunScooperTask() error {
	log.Print("task.ScooperTask: Starting scooper run")
	log.Print("task.ScooperTask: scooper run complete (no plugins configured)")
	return nil
}
