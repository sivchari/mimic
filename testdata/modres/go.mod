module example.com/modres

go 1.26

require (
	example.com/Upper v1.0.0
	example.com/anyv v1.0.0
	example.com/cached v1.2.0
	example.com/localrep v0.0.0
	example.com/missing v1.0.0
	example.com/orig v1.0.0
)

replace example.com/localrep => ./replaced/localrep

replace example.com/orig v1.0.0 => example.com/forked v1.5.0

replace example.com/anyv => example.com/forked v1.5.0
