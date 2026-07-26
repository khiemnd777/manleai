package fence

const AdvisoryKeyPrefix = "booking-calendar-reconciliation:"

func AdvisoryKey(salonID string) string {
	return AdvisoryKeyPrefix + salonID
}
