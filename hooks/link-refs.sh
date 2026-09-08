#!/bin/sh
# The reference linker. The rule is slopfix's `linkrefs` package, reached
# through `slopfix link-refs`, which reads the MessageDisplay payload on stdin
# and writes the displayContent envelope on stdout.
#
# It renders a pull request, a commit or a branch as a markdown link while the
# message streams, and sends nothing back to the model. A missing link is not
# work only the model can do: the token plus the checkout determine the URL, so
# the hook writes it. No round trip, and nothing to loop on.
#
# A pull request carries a status dot, so the reader learns what the page is
# doing without opening it. Merged is the one state where the link MOVES: the
# words go back to plain text and the dot carries the route, because a link is
# a demand to stop reading and move your hand, and a merged page has nothing
# left to do.
#
# The lookup rides GH_HOST, which points at the state mirror. There is no
# hostname flag, no URL and no api.github.com literal anywhere in the package.
exec "$(dirname "$0")/../bin/slopfix.ape" link-refs
