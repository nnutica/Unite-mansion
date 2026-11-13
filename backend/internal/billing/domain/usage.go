package domain

// UsageCheck is the abnormal-usage warning for one meter (§6.2). It never
// blocks saving; the admin confirms in M-03.
type UsageCheck struct {
	Abnormal bool
	// PercentOfAverage is current units as a percentage of the average
	// (e.g. 250 = 2.5×). Nil when there is no history or the average is 0.
	PercentOfAverage *int64
}

// CheckUsage flags current units above the average of history by more than
// 100% (i.e. more than twice the average). history holds the units of up to
// the 3 latest issued invoices before this one; with no history there is no
// warning. A zero average with any usage is flagged.
func CheckUsage(current int64, history []int64) UsageCheck {
	if len(history) == 0 {
		return UsageCheck{}
	}
	var sum int64
	for _, h := range history {
		sum += h
	}
	n := int64(len(history))
	if sum == 0 {
		return UsageCheck{Abnormal: current > 0}
	}
	// current > 2 × (sum / n)  ⇔  current × n > 2 × sum, in integers.
	percent := current * 100 * n / sum
	return UsageCheck{Abnormal: current*n > 2*sum, PercentOfAverage: &percent}
}
