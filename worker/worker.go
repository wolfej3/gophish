package worker

import (
	"context"
	"time"

	"github.com/gophish/gophish/config"
	"github.com/gophish/gophish/directory"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/mailer"
	"github.com/gophish/gophish/models"
	"github.com/sirupsen/logrus"
)

// Worker is an interface that defines the operations needed for a background worker
type Worker interface {
	Start()
	LaunchCampaign(c models.Campaign)
	SendTestEmail(s *models.EmailRequest) error
}

// DefaultWorker is the background worker that handles watching for new campaigns and sending emails appropriately.
type DefaultWorker struct {
	mailer     mailer.Mailer
	connectors []config.DirectoryConnector
}

// New creates a new worker object to handle the creation of campaigns
func New(options ...func(*DefaultWorker) error) (Worker, error) {
	defaultMailer := mailer.NewMailWorker()
	w := &DefaultWorker{
		mailer: defaultMailer,
	}
	for _, opt := range options {
		if err := opt(w); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// WithMailer sets the mailer for a given worker.
// By default, workers use a standard, default mailworker.
func WithMailer(m mailer.Mailer) func(*DefaultWorker) error {
	return func(w *DefaultWorker) error {
		w.mailer = m
		return nil
	}
}

// WithDirectoryConnectors sets the directory connectors the worker uses to run
// scheduled group syncs.
func WithDirectoryConnectors(connectors []config.DirectoryConnector) func(*DefaultWorker) error {
	return func(w *DefaultWorker) error {
		w.connectors = connectors
		return nil
	}
}

// connectorByName returns the configured directory connector with the given
// name, if any.
func (w *DefaultWorker) connectorByName(name string) (config.DirectoryConnector, bool) {
	for _, c := range w.connectors {
		if c.Name == name {
			return c, true
		}
	}
	return config.DirectoryConnector{}, false
}

// syncDueDirectorySources refreshes the members of each group whose directory
// sync interval has elapsed, keeping campaign recipient lists current without
// operator intervention.
func (w *DefaultWorker) syncDueDirectorySources(t time.Time) {
	sources, err := models.GetDueDirectorySources(t)
	if err != nil {
		log.Error(err)
		return
	}
	for _, source := range sources {
		conn, ok := w.connectorByName(source.Connector)
		if !ok {
			log.WithFields(logrus.Fields{
				"connector": source.Connector,
				"group_id":  source.GroupId,
			}).Warn("Skipping scheduled sync: connector is no longer configured")
			continue
		}
		group, err := models.GetGroupById(source.GroupId)
		if err != nil {
			log.Error(err)
			continue
		}
		log.WithFields(logrus.Fields{
			"group":     group.Name,
			"connector": source.Connector,
		}).Info("Running scheduled directory sync")
		// SyncGroup records the outcome (and LastSyncedAt) on the source, which
		// determines when it next comes due.
		if err := directory.SyncGroup(conn, source, &group); err != nil {
			log.WithFields(logrus.Fields{
				"group": group.Name,
			}).Errorf("scheduled directory sync failed: %v", err)
		}
	}
}

// processCampaigns loads maillogs scheduled to be sent before the provided
// time and sends them to the mailer.
func (w *DefaultWorker) processCampaigns(t time.Time) error {
	ms, err := models.GetQueuedMailLogs(t.UTC())
	if err != nil {
		log.Error(err)
		return err
	}
	// Lock the MailLogs (they will be unlocked after processing)
	err = models.LockMailLogs(ms, true)
	if err != nil {
		return err
	}
	campaignCache := make(map[int64]models.Campaign)
	// We'll group the maillogs by campaign ID to (roughly) group
	// them by sending profile. This lets the mailer re-use the Sender
	// instead of having to re-connect to the SMTP server for every
	// email.
	msg := make(map[int64][]mailer.Mail)
	for _, m := range ms {
		// We cache the campaign here to greatly reduce the time it takes to
		// generate the message (ref #1726)
		c, ok := campaignCache[m.CampaignId]
		if !ok {
			c, err = models.GetCampaignMailContext(m.CampaignId, m.TeamId)
			if err != nil {
				return err
			}
			campaignCache[c.Id] = c
		}
		m.CacheCampaign(&c)
		msg[m.CampaignId] = append(msg[m.CampaignId], m)
	}

	// Next, we process each group of maillogs in parallel
	for cid, msc := range msg {
		go func(cid int64, msc []mailer.Mail) {
			c := campaignCache[cid]
			if c.Status == models.CampaignQueued {
				err := c.UpdateStatus(models.CampaignInProgress)
				if err != nil {
					log.Error(err)
					return
				}
			}
			log.WithFields(logrus.Fields{
				"num_emails": len(msc),
			}).Info("Sending emails to mailer for processing")
			w.mailer.Queue(msc)
		}(cid, msc)
	}
	return nil
}

// launchDueSchedules spawns a campaign from each recurring schedule whose next
// launch time has arrived, then advances the schedule to its next occurrence.
// Each run resolves the schedule's template, page, sending profile, and groups
// by name, so it picks up current group membership at launch time.
func (w *DefaultWorker) launchDueSchedules(t time.Time) {
	schedules, err := models.GetDueSchedules(t)
	if err != nil {
		log.Error(err)
		return
	}
	for i := range schedules {
		s := &schedules[i]
		c := s.BuildCampaign(t.UTC())
		err := models.PostCampaign(&c, s.UserId, s.TeamId)
		if err != nil {
			log.WithFields(logrus.Fields{
				"schedule": s.Name,
			}).Errorf("error launching scheduled campaign: %v", err)
			// Advance anyway so a broken schedule doesn't retry every minute.
			if aerr := s.AdvanceSchedule(t); aerr != nil {
				log.Error(aerr)
			}
			continue
		}
		log.WithFields(logrus.Fields{
			"schedule": s.Name,
			"campaign": c.Name,
		}).Info("Launching scheduled campaign")
		if c.Status == models.CampaignInProgress {
			w.LaunchCampaign(c)
		}
		if aerr := s.AdvanceSchedule(t); aerr != nil {
			log.Error(aerr)
		}
	}
}

// Start launches the worker to poll the database every minute for any pending maillogs
// that need to be processed.
func (w *DefaultWorker) Start() {
	log.Info("Background Worker Started Successfully - Waiting for Campaigns")
	go w.mailer.Start(context.Background())
	for t := range time.Tick(1 * time.Minute) {
		w.launchDueSchedules(t)
		w.syncDueDirectorySources(t)
		err := w.processCampaigns(t)
		if err != nil {
			log.Error(err)
			continue
		}
	}
}

// LaunchCampaign starts a campaign
func (w *DefaultWorker) LaunchCampaign(c models.Campaign) {
	ms, err := models.GetMailLogsByCampaign(c.Id)
	if err != nil {
		log.Error(err)
		return
	}
	models.LockMailLogs(ms, true)
	// This is required since you cannot pass a slice of values
	// that implements an interface as a slice of that interface.
	mailEntries := []mailer.Mail{}
	currentTime := time.Now().UTC()
	campaignMailCtx, err := models.GetCampaignMailContext(c.Id, c.TeamId)
	if err != nil {
		log.Error(err)
		return
	}
	for _, m := range ms {
		// Only send the emails scheduled to be sent for the past minute to
		// respect the campaign scheduling options
		if m.SendDate.After(currentTime) {
			m.Unlock()
			continue
		}
		err = m.CacheCampaign(&campaignMailCtx)
		if err != nil {
			log.Error(err)
			return
		}
		mailEntries = append(mailEntries, m)
	}
	w.mailer.Queue(mailEntries)
}

// SendTestEmail sends a test email
func (w *DefaultWorker) SendTestEmail(s *models.EmailRequest) error {
	go func() {
		ms := []mailer.Mail{s}
		w.mailer.Queue(ms)
	}()
	return <-s.ErrorChan
}
