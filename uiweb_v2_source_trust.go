package gextto

type v2SourceTrustView struct {
	Days        int
	DaysOptions []int
	Sources     []SourceTrustStat
	Finals      []TransferFinal
	Error       string
}

func v2SourceTrustViewFrom(s *AppState, days int) v2SourceTrustView {
	view := v2SourceTrustView{Days: days, DaysOptions: []int{1, 7, 30, 90}, Sources: []SourceTrustStat{}, Finals: []TransferFinal{}}
	report, err := s.db.SourceTrustReport(days)
	if err != nil {
		view.Error = "Lettura delle statistiche di affidabilità non riuscita."
		return view
	}
	view.Sources = report.Sources
	view.Finals = report.Finals
	return view
}
