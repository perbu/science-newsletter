package openalex

// Author represents an OpenAlex author record.
type Author struct {
	ID              string        `json:"id"`
	DisplayName     string        `json:"display_name"`
	WorksCount      int           `json:"works_count"`
	CitedByCount    int           `json:"cited_by_count"`
	SummaryStats    SummaryStats  `json:"summary_stats"`
	Affiliations    []Affiliation `json:"affiliations"`
	Topics          []AuthorTopic `json:"topics"`
	LastKnownInstitutions []Institution `json:"last_known_institutions"`
}

type SummaryStats struct {
	HIndex int `json:"h_index"`
}

type Affiliation struct {
	Institution Institution `json:"institution"`
}

type Institution struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type AuthorTopic struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Subfield    Subfield `json:"subfield"`
	Field       Field    `json:"field"`
	Domain      Domain   `json:"domain"`
	Count       int      `json:"count"`
	Score       float64  `json:"score"`
}

type Subfield struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Field struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Domain struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Location represents the primary location of a work (where it was published).
type Location struct {
	Source *LocationSource `json:"source"`
}

// LocationSource is the source (journal/repository) within a location.
type LocationSource struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// SourceDetail is a full source record from the /sources endpoint.
type SourceDetail struct {
	ID           string      `json:"id"`
	DisplayName  string      `json:"display_name"`
	SummaryStats SourceStats `json:"summary_stats"`
}

// SourceStats contains citation metrics for a source.
type SourceStats struct {
	TwoYrMeanCitedness float64 `json:"2yr_mean_citedness"`
}

// SourcesResponse wraps paginated /sources results.
type SourcesResponse struct {
	Results []SourceDetail `json:"results"`
	Meta    Meta           `json:"meta"`
}

// Work represents an OpenAlex work (publication).
type Work struct {
	ID                    string           `json:"id"`
	Title                 string           `json:"title"`
	DOI                   string           `json:"doi"`
	PublicationDate       string           `json:"publication_date"`
	CitedByCount          int              `json:"cited_by_count"`
	PrimaryLocation       *Location        `json:"primary_location"`
	Authorships           []Authorship     `json:"authorships"`
	Topics                []WorkTopic      `json:"topics"`
	ReferencedWorks       []string         `json:"referenced_works"`
	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
}

// AbstractText reconstructs the abstract from the inverted index.
func (w *Work) AbstractText() string {
	if len(w.AbstractInvertedIndex) == 0 {
		return ""
	}
	// Find max position
	max := 0
	for _, positions := range w.AbstractInvertedIndex {
		for _, p := range positions {
			if p > max {
				max = p
			}
		}
	}
	words := make([]string, max+1)
	for word, positions := range w.AbstractInvertedIndex {
		for _, p := range positions {
			words[p] = word
		}
	}
	result := ""
	for i, w := range words {
		if i > 0 && w != "" {
			result += " "
		}
		result += w
	}
	return result
}

type Authorship struct {
	Author      AuthorRef    `json:"author"`
	Institutions []Institution `json:"institutions"`
}

type AuthorRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type WorkTopic struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Subfield    Subfield `json:"subfield"`
	Field       Field    `json:"field"`
	Domain      Domain   `json:"domain"`
	Score       float64 `json:"score"`
}

// Response wrappers for paginated endpoints.
type AuthorSearchResponse struct {
	Results []Author `json:"results"`
	Meta    Meta     `json:"meta"`
}

type WorksResponse struct {
	Results []Work `json:"results"`
	Meta    Meta   `json:"meta"`
}

type Meta struct {
	Count      int    `json:"count"`
	PerPage    int    `json:"per_page"`
	NextCursor string `json:"next_cursor"`
}

// TopicSearchResult represents a topic returned from the /topics search endpoint.
type TopicSearchResult struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Subfield    Subfield `json:"subfield"`
	Field       Field    `json:"field"`
	Domain      Domain   `json:"domain"`
}

// TopicsSearchResponse wraps paginated /topics results.
type TopicsSearchResponse struct {
	Results []TopicSearchResult `json:"results"`
	Meta    Meta                `json:"meta"`
}
