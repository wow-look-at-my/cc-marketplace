#!/bin/sh
# The prose-question guard. The rule is slopfix's `askproperly` package,
# reached through `slopfix ask-properly`.
#
# A closing message that hands the reader a decision in prose gets one line
# appended to what the reader sees. It refuses nothing: the reader is the
# person the question was aimed at, so the note goes there.
exec "$(dirname "$0")/../bin/slopfix.ape" ask-properly
