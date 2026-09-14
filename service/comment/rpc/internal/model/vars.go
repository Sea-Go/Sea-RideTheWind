package model

import "errors"

var (
	ErrorSubjectNotFound  = errors.New("subject not found")
	ErrorCommentNotFound  = errors.New("comment not found")
	ErrorCommentForbidden = errors.New("comment operation forbidden")
)
