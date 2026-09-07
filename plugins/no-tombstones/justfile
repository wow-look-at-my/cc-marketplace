[private]
help:
	@just --list

# Fetch the slopfix this plugin runs, and prove the rule fires before shipping.
# A plugin whose guard cannot run must not publish: the alternative is a guard
# that installs, reports success and does nothing.
prebuild:
	cd ../.. && sh .github/scripts/vendor-slopfix.sh plugins/no-tombstones tombstones
