.PHONY: help wg-keys clean s0 s1 s2 s3 s5 s7 s9 all

help:
	@echo "targets:"
	@echo "  wg-keys     generate WireGuard keypairs (required before wg stack)"
	@echo "  s1 BE=wg    run S1 on backend BE (b0|wg|fr|ch)"
	@echo "  s2 BE=..    run S2"
	@echo "  s3 BE=..    run S3"
	@echo "  s7 BE=..    run S7 (dynamic add disruption)"
	@echo "  s5 BE=wg    run S5 (wg only for now)"
	@echo "  s9 BE=..    run S9 (reconnect)"
	@echo "  all         run run_all.sh sweep"
	@echo "  clean       tear down any leftover compose stacks + results/raw"

BE ?= wg

wg-keys:
	sh configs/wireguard/keygen.sh

s0:; bash scenarios/s0_baseline.sh
s1:; bash scenarios/s1_throughput.sh $(BE)
s2:; bash scenarios/s2_parallel.sh   $(BE)
s3:; bash scenarios/s3_http.sh       $(BE)
s5:; bash scenarios/s5_idle_scale.sh $(BE)
s7:; bash scenarios/s7_dynamic_add.sh $(BE)
s9:; bash scenarios/s9_reconnect.sh  $(BE)

all:; bash scenarios/run_all.sh

clean:
	-for f in compose/*.yml; do docker compose -f $$f down -v --remove-orphans 2>/dev/null; done
	rm -rf results/raw
