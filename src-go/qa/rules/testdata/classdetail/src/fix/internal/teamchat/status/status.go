// Package status is the record's problem, as teamchat's status package has it.
package status

// Problem is a problem on a team's record.
type Problem struct {
	What, Fix string
}

// Other is a type of the same package that classRecords does not list.
type Other struct {
	What string
}
