#!/usr/bin/env python3
"""Read Linux /proc metrics for the opt-in capacity test; never read process env."""
import argparse,json,os,pathlib,time
p=argparse.ArgumentParser();p.add_argument('--output',required=True);p.add_argument('--seconds',type=int,default=960);a=p.parse_args()
start=time.monotonic();target=None;samples=[];clock_ticks=os.sysconf('SC_CLK_TCK');page_size=os.sysconf('SC_PAGE_SIZE')
while time.monotonic()-start<a.seconds:
 if target is None:
  for candidate in pathlib.Path('/proc').iterdir():
   if not candidate.name.isdigit():continue
   try:
    if candidate.joinpath('comm').read_text().strip()=='httpapi.test' and b'TestCapacityHTTP' in candidate.joinpath('cmdline').read_bytes():target=candidate;break
   except OSError:pass
 if target is not None:
  try:
   raw=target.joinpath('stat').read_text();values=raw[raw.rindex(')')+2:].split();samples.append({'elapsedSeconds':round(time.monotonic()-start,2),'cpuSeconds':(int(values[11])+int(values[12]))/clock_ticks,'rssBytes':int(values[21])*page_size})
  except OSError:break
 time.sleep(10)
report={'scope':'Combined isolated test generator/API/worker process; excludes PostgreSQL and other services','samples':samples,'peakObservedRSSBytes':max((v['rssBytes'] for v in samples),default=0),'sampleIntervalSeconds':10}
pathlib.Path(a.output).write_text(json.dumps(report,indent=2)+'\n')
