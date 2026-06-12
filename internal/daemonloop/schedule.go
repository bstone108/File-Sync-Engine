package daemonloop

import "time"

// PollSchedule tracks independent daemon loop deadlines for recurring work.
type PollSchedule struct {
	nextDiscovery time.Time
	nextMetadata  time.Time
}

func NewPollSchedule(nextDiscovery, nextMetadata time.Time) PollSchedule {
	return PollSchedule{nextDiscovery: nextDiscovery, nextMetadata: nextMetadata}
}

func (s *PollSchedule) DiscoveryDue(now time.Time, interval time.Duration) bool {
	if now.Before(s.nextDiscovery) {
		return false
	}
	s.nextDiscovery = now.Add(interval)
	return true
}

func (s *PollSchedule) MetadataDue(now time.Time, interval time.Duration) bool {
	if now.Before(s.nextMetadata) {
		return false
	}
	s.nextMetadata = now.Add(interval)
	return true
}

func (s PollSchedule) NextDiscovery() time.Time {
	return s.nextDiscovery
}

func (s PollSchedule) NextMetadata() time.Time {
	return s.nextMetadata
}

// DueWorkOptions contains the recurring daemon work callbacks for one poll tick.
type DueWorkOptions struct {
	DiscoveryInterval time.Duration
	MetadataInterval  time.Duration
	Discovery         func()
	Metadata          func()
}

// DueWorkResult reports which recurring actions ran during a poll tick.
type DueWorkResult struct {
	DiscoveryRan bool
	MetadataRan  bool
}

// RunDueWork evaluates one daemon poll tick and runs due recurring actions in a
// stable order. The schedule deadlines advance from the tick time, not from the
// completion time of callback work.
func RunDueWork(schedule *PollSchedule, now time.Time, opts DueWorkOptions) DueWorkResult {
	result := DueWorkResult{}
	if schedule.DiscoveryDue(now, opts.DiscoveryInterval) {
		result.DiscoveryRan = true
		if opts.Discovery != nil {
			opts.Discovery()
		}
	}
	if schedule.MetadataDue(now, opts.MetadataInterval) {
		result.MetadataRan = true
		if opts.Metadata != nil {
			opts.Metadata()
		}
	}
	return result
}
