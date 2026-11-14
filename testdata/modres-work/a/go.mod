module example.com/work/a

go 1.26

require (
	example.com/cached v1.1.0
	example.com/over v1.0.0
)

replace example.com/over => ./a-over
