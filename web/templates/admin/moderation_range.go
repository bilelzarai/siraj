package admin

// pageFrom and pageTo are the first and last row of a page, for the "Showing a–b of
// n" line under a list that pages on its own parameter (A10's two).
func pageFrom(page, pages, total int) int {
	if total == 0 {
		return 0
	}
	return (page-1)*HealthPerPage + 1
}

func pageTo(page, pages, total int) int {
	end := page * HealthPerPage
	if end > total {
		return total
	}
	return end
}

// HealthPerPage mirrors the service's page size so the range and the rows agree.
const HealthPerPage = 20
