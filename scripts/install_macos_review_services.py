#!/usr/bin/env python3
"""Install the three local review services without embedding credentials.

Default: current-user LaunchAgents supporting GUI and background login.
--system: boot-time LaunchDaemons running as the workspace owner (requires sudo).
--dry-run: validate and print plans without writing or starting services.
"""
import argparse
import os
from pathlib import Path
import plistlib
import pwd
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--system', action='store_true')
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    runtime = root / '.runtime'
    owner = pwd.getpwuid(runtime.stat().st_uid)
    if owner.pw_uid == 0:
        parser.error('workspace must belong to a non-root service user')
    if not args.dry_run:
        if args.system and os.geteuid() != 0:
            parser.error('--system requires sudo')
        if not args.system and os.geteuid() != owner.pw_uid:
            parser.error('user install must run as the workspace owner')
    destination = Path('/Library/LaunchDaemons') if args.system else Path(owner.pw_dir) / 'Library/LaunchAgents'
    domain = 'system' if args.system else 'user/' + str(owner.pw_uid)
    plans = []
    for suffix, config, log in [('', 'config.json', 'service.log'), ('-oasis', 'config-oasis.json', 'oasis-service.log'), ('-nx2800', 'config-nx2800.json', 'nx2800-service.log')]:
        for name in ['run.py', config]:
            if not (runtime / name).is_file():
                parser.error('missing ' + str(runtime / name))
        label = 'com.halliday.hermes-review-gate' + suffix
        data = dict(Label=label, ProgramArguments=['/usr/bin/python3', str(runtime / 'run.py'), '-config', str(runtime / config), 'run'], WorkingDirectory=str(root), KeepAlive=True, RunAtLoad=True, ProcessType='Background', ThrottleInterval=10, StandardOutPath=str(runtime / log), StandardErrorPath=str(runtime / log))
        if args.system:
            data['UserName'] = owner.pw_name
        else:
            data['LimitLoadToSessionType'] = ['Aqua', 'Background']
        encoded = plistlib.dumps(data)
        assert plistlib.loads(encoded) == data
        plans.append((label, destination / (label + '.plist'), encoded))
    if args.dry_run:
        for label, path, _ in plans:
            print(domain + '/' + label + ' -> ' + str(path) + ' (user=' + owner.pw_name + ')')
        return
    destination.mkdir(parents=True, exist_ok=True)
    for label, path, encoded in plans:
        # Migration keeps one writer per state directory. State locks additionally
        # prevent accidental duplicate controllers from publishing reviews.
        domains = ['gui/' + str(owner.pw_uid), 'user/' + str(owner.pw_uid)]
        if args.system:
            domains.append('system')
        for old in domains:
            subprocess.run(['launchctl', 'bootout', old + '/' + label], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if args.system:
            agent = Path(owner.pw_dir) / 'Library/LaunchAgents' / (label + '.plist')
            if agent.exists():
                agent.rename(agent.with_suffix('.plist.disabled'))
        path.write_bytes(encoded)
        path.chmod(0o644)
        if args.system:
            os.chown(path, 0, 0)
        subprocess.run(['launchctl', 'bootstrap', domain, str(path)], check=True)
        print('Installed ' + domain + '/' + label)


if __name__ == '__main__':
    main()
