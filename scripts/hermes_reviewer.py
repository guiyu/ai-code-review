#!/usr/bin/env python3
"""Hermes AIAgent adapter. Only immutable stdin evidence is exposed to the agent."""
import contextlib
import importlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import sys
import tempfile
from urllib.parse import urlsplit

MAX_INPUT = 524288
MAX_OUTPUT = 131072
SCOPE = '证据范围：仅限远端 PR 差异与已提供的提交日志、元数据；未读取完整源码、关联仓库或本机未提交修改，未执行构建和真机测试。'


class ReviewError(Exception):
    pass


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ReviewError('duplicate JSON key')
        result[key] = value
    return result


def decode(raw):
    return json.loads(raw, object_pairs_hook=unique_object,
                      parse_constant=lambda _: (_ for _ in ()).throw(ReviewError('nonfinite JSON')))


def bounded_integer(env, name, default, minimum, maximum):
    value = int(env.get(name, default))
    if not minimum <= value <= maximum:
        raise ReviewError('invalid limit')
    return value


def configuration(env):
    config = {key: env.get('REVIEW_' + key, '') for key in ('MODEL', 'BASE_URL', 'API_KEY', 'HERMES_PATH')}
    if any(not value.strip() for value in config.values()):
        raise ReviewError('missing REVIEW configuration')
    url = urlsplit(config['BASE_URL'])
    if url.scheme not in ('https', 'http') or not url.hostname or url.username or url.password or url.query or url.fragment:
        raise ReviewError('invalid model endpoint')
    source = Path(config['HERMES_PATH'])
    if not source.is_absolute() or not (source / 'run_agent.py').is_file():
        raise ReviewError('invalid Hermes source path')
    config['MAX_INPUT_BYTES'] = bounded_integer(env, 'REVIEW_MAX_INPUT_BYTES', MAX_INPUT, 1024, MAX_INPUT)
    config['MAX_TOKENS'] = bounded_integer(env, 'REVIEW_MAX_TOKENS', 8192, 1024, 16384)
    config['MAX_ITERATIONS'] = bounded_integer(env, 'REVIEW_MAX_ITERATIONS', 16, 2, 64)
    config['TIMEOUT_SECONDS'] = bounded_integer(env, 'REVIEW_TIMEOUT_SECONDS', 240, 1, 1800)
    return config


def safe_path(value):
    p = PurePosixPath(value)
    return bool(value) and not p.is_absolute() and '..' not in p.parts and '\\' not in value and '\x00' not in value


def parse_diff(diff):
    """Validate complete textual unified hunks and record allowable finding lines.

    Binary patches and quoted/combined paths are deliberately unsupported: a gate
    must request another review path instead of approving uninspected content.
    """
    lines = diff.splitlines()
    locations = {}
    file = None
    old_path = None
    remaining_old = remaining_new = 0
    old_line = new_line = 0
    in_hunk = False
    headers = 0
    hunks = 0
    file_hunks = 0
    saw_old = saw_new = False
    for line in lines:
        if line == '\\ No newline at end of file':
            if not in_hunk:
                raise ReviewError('invalid newline marker')
            continue
        if in_hunk and (remaining_old or remaining_new):
            if not line or line[0] not in ' +-':
                raise ReviewError('incomplete diff hunk')
            kind = line[0]
            if kind in ' -':
                remaining_old -= 1
                if kind == '-':
                    locations[file].add(old_line)
                old_line += 1
            if kind in ' +':
                remaining_new -= 1
                locations[file].add(new_line)
                new_line += 1
            if remaining_old < 0 or remaining_new < 0:
                raise ReviewError('invalid diff hunk count')
            continue
        if line.startswith('diff --git '):
            if headers and not file_hunks:
                raise ReviewError('file has no complete textual hunk')
            file_hunks = 0
            saw_old = saw_new = False
            headers += 1
            file = None
            old_path = None
            in_hunk = False
        elif line.startswith('--- '):
            if not headers or saw_old or saw_new or in_hunk:
                raise ReviewError('unexpected old-file header')
            saw_old = True
            value = line[4:]
            if value != '/dev/null' and not value.startswith('a/'):
                raise ReviewError('unsupported diff path')
            old_path = value[2:] if value != '/dev/null' else None
        elif line.startswith('+++ '):
            if not saw_old or saw_new or in_hunk:
                raise ReviewError('unexpected new-file header')
            saw_new = True
            value = line[4:]
            if value != '/dev/null' and not value.startswith('b/'):
                raise ReviewError('unsupported diff path')
            file = old_path if value == '/dev/null' else value[2:]
            if not file or not safe_path(file):
                raise ReviewError('unsafe diff path')
            locations.setdefault(file, set())
        elif line.startswith('@@ '):
            match = re.match(r'^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?:.*)$', line)
            if not match or file is None:
                raise ReviewError('invalid diff hunk')
            old_line, old_count, new_line, new_count = match.groups()
            old_line, new_line = int(old_line), int(new_line)
            remaining_old = int(old_count) if old_count is not None else 1
            remaining_new = int(new_count) if new_count is not None else 1
            in_hunk = True
            hunks += 1
            file_hunks += 1
        elif line.startswith(('index ', 'new file mode ', 'deleted file mode ', 'old mode ', 'new mode ', 'similarity index ', 'dissimilarity index ', 'rename from ', 'rename to ', 'copy from ', 'copy to ')):
            if not headers or saw_old or saw_new or in_hunk:
                raise ReviewError('unexpected diff metadata')
        else:
            raise ReviewError('unsupported or truncated diff')
    if remaining_old or remaining_new or not headers or not hunks or not file_hunks:
        raise ReviewError('missing or incomplete text hunks')
    return lines, locations


def validate_input(value):
    required = {'repository', 'number', 'head_sha', 'base_sha', 'diff', 'title'}
    optional = {'description', 'head_ref', 'base_ref', 'merge_base', 'commits'}
    if not isinstance(value, dict) or not required <= set(value) or set(value) - required - optional:
        raise ReviewError('invalid input object')
    if type(value['number']) is not int or value['number'] <= 0:
        raise ReviewError('invalid PR number')
    if not isinstance(value['repository'], str) or not re.fullmatch(r'[^/\s]+/[^/\s]+', value['repository']):
        raise ReviewError('invalid repository')
    for key in ('head_sha', 'base_sha'):
        if not isinstance(value[key], str) or not re.fullmatch(r'[a-fA-F0-9]{40,64}', value[key]) or len(value[key]) not in (40, 64):
            raise ReviewError('invalid revision')
    if not isinstance(value['title'], str) or len(value['title']) > 4096 or not isinstance(value['diff'], str):
        raise ReviewError('invalid text input')
    for key in ('description', 'head_ref', 'base_ref', 'merge_base'):
        if key in value and (not isinstance(value[key], str) or len(value[key]) > 16000):
            raise ReviewError('invalid PR metadata')
    commits = value.get('commits', [])
    if not isinstance(commits, list) or len(commits) > 1000:
        raise ReviewError('invalid commit history')
    for commit in commits:
        if not isinstance(commit, dict) or set(commit) != {'sha', 'message'}:
            raise ReviewError('invalid commit')
        if not isinstance(commit['sha'], str) or not re.fullmatch(r'[a-f0-9]{40}|[a-f0-9]{64}', commit['sha']):
            raise ReviewError('invalid commit SHA')
        if not isinstance(commit['message'], str) or not commit['message'].strip() or len(commit['message']) > 16000:
            raise ReviewError('invalid commit message')
    return parse_diff(value['diff'])


class Evidence:
    def __init__(self, lines):
        self.lines = tuple(lines)
        self.seen = set()

    def dispatch(self, name, args, *unused, **kwargs):
        if name != 'read_diff':
            return json.dumps({'error': 'Only read_diff is permitted.'})
        if not isinstance(args, dict) or set(args) != {'start_line', 'line_count'}:
            return json.dumps({'error': 'Provide start_line and line_count.'})
        start, count = args['start_line'], args['line_count']
        if type(start) is not int or type(count) is not int or start < 1 or start > len(self.lines) or not 1 <= count <= 200:
            return json.dumps({'error': 'Invalid range; at most 200 lines per read.'})
        end = min(start - 1 + count, len(self.lines))
        self.seen.update(range(start - 1, end))
        return json.dumps({'start_line': start, 'end_line': end, 'total_lines': len(self.lines), 'text': '\n'.join(self.lines[start - 1:end])})


TOOL = {'type': 'function', 'function': {'name': 'read_diff', 'description': 'Read an immutable range of the supplied unified diff. No filesystem access. Read every line before completing.', 'parameters': {'type': 'object', 'properties': {'start_line': {'type': 'integer', 'minimum': 1}, 'line_count': {'type': 'integer', 'minimum': 1, 'maximum': 200}}, 'required': ['start_line', 'line_count'], 'additionalProperties': False}}}
REPORT_SECTIONS = ('评审范围', '修改目标与实现分析', '代码问题清单', '跨仓影响范围', '修改完整性评估', '历史问题回归评估', '构建与真机验证矩阵')
RECOMMENDATIONS = {'通过': '可以合入', '有条件通过': '完成指定验证后合入', '不通过': '修复后重新评审', '证据不足': '拒绝合入'}
PRIORITIES = {'critical': 'P0', 'high': 'P1', 'medium': 'P2', 'low': 'P3', 'info': '提示'}
# Trusted deployment file, never read from PR-controlled source or model input.
SYSTEM = Path(__file__).resolve().parent.parent.joinpath('prompts/oasis-review.md').read_text(encoding='utf-8') + """

机器输出协议（必须遵守）：
必须通过 read_diff 读取全部差异行。只返回一个 JSON 对象，不使用 JSON 代码块：
{"complete":true,"verdict":"通过|有条件通过|不通过|证据不足","summary":"七个章节的中文 Markdown 正文","findings":[{"severity":"critical|high|medium|low|info","file":"相对路径","line":1,"title":"中文问题标题","evidence":"中文：仓库、函数、触发条件、调用链、状态变化、后果、置信度、历史关联；缺失证据明确说明","suggestion":"中文修复建议及验证方法"}]}
complete 表示已完成对可用证据的评审，不等同于具备完整跨仓证据或允许合入；证据不足也要返回 complete:true、verdict:证据不足和正式报告。
summary 必须依次包含以下独立二级标题，且每节有中文内容：
## 评审范围
## 修改目标与实现分析
## 代码问题清单
## 跨仓影响范围
## 修改完整性评估
## 历史问题回归评估
## 构建与真机验证矩阵
报告总标题、评审结论、最高风险和最终合入建议由控制器根据 verdict/findings 确定性生成，请勿在 summary 另加这些章节或自行输出相反的合入建议。
已证实的问题必须同时放入 findings，引用 diff 中实际出现的文件和行号；不要只在 summary 写阻断问题却留下空 findings。critical/high/medium/low 对应 P0/P1/P2/P3。存在 P0/P1 时不得给出“通过”。所有人类可读结论、证据和建议使用中文，保留代码符号、路径和机器枚举原文。不得把缺少证据编造成某个 diff 行的缺陷。
"""


@contextlib.contextmanager
def quiet_library():
    """Suppress third-party diagnostics at descriptor level, including secrets."""
    sys.stdout.flush()
    sys.stderr.flush()
    out, err = os.dup(1), os.dup(2)
    try:
        with open(os.devnull, 'w') as sink:
            os.dup2(sink.fileno(), 1)
            os.dup2(sink.fileno(), 2)
            try:
                yield
            finally:
                sys.stdout.flush()
                sys.stderr.flush()
    finally:
        os.dup2(out, 1)
        os.dup2(err, 2)
        os.close(out)
        os.close(err)


def run_agent(config, value, evidence):
    # This program is a one-run subprocess. Never import Hermes before isolation.
    with tempfile.TemporaryDirectory(prefix='hermes-review-') as home:
        home = str(Path(home).resolve())
        os.chmod(home, 0o700)
        os.environ.clear()
        os.environ.update(HOME=home, HERMES_HOME=home + '/.hermes', HERMES_MANAGED_DIR=home + '/.managed', XDG_CONFIG_HOME=home + '/.config', XDG_CACHE_HOME=home + '/.cache', PATH='/usr/bin:/bin', LANG='C.UTF-8', HERMES_API_TIMEOUT=str(config['TIMEOUT_SECONDS']), PYTHONDONTWRITEBYTECODE='1')
        previous_cwd = os.getcwd()
        os.chdir(home)
        sys.dont_write_bytecode = True
        sys.path.insert(0, config['HERMES_PATH'])
        try:
            with quiet_library():
                # Installed Hermes otherwise reads checkout .env and managed
                # credential sources, even with an empty temporary HERMES_HOME.
                loader = importlib.import_module('hermes_cli.env_loader')
                loader.load_hermes_dotenv = lambda *args, **kwargs: []
                module = importlib.import_module('run_agent')
                # Deny dispatch independently of the tools advertised to the model.
                module.handle_function_call = evidence.dispatch
                agent = module.AIAgent(model=config['MODEL'], base_url=config['BASE_URL'], api_key=config['API_KEY'], provider='custom', api_mode='chat_completions', max_iterations=config['MAX_ITERATIONS'], max_tokens=config['MAX_TOKENS'], enabled_toolsets=[], skip_context_files=True, skip_memory=True, load_soul_identity=False, save_trajectories=False, verbose_logging=False, quiet_mode=True, checkpoints_enabled=False, fallback_model=None)
                agent.tools = [TOOL]
                agent.valid_tool_names = {'read_diff'}
                metadata = {key: val for key, val in value.items() if key != 'diff'}
                metadata['diff_line_count'] = len(evidence.lines)
                return agent.run_conversation(user_message=json.dumps(metadata), system_message=SYSTEM)
        finally:
            os.chdir(previous_cwd)


def validate_completion(result):
    if not isinstance(result, dict):
        raise ReviewError('Hermes did not complete')
    for message in result.get('messages', []):
        if isinstance(message, dict) and message.get('finish_reason') in ('length', 'incomplete', 'content_filter'):
            raise ReviewError('truncated or filtered response')
    if result.get('completed') is not True or result.get('error'):
        raise ReviewError('Hermes did not complete')


def validate_output(result, evidence, locations):
    validate_completion(result)
    if len(evidence.seen) != len(evidence.lines):
        raise ReviewError('incomplete diff coverage')
    raw = result.get('final_response')
    if not isinstance(raw, str) or len(raw.encode('utf-8')) > MAX_OUTPUT:
        raise ReviewError('invalid response size')
    output = decode(raw)
    if not isinstance(output, dict) or set(output) != {'complete', 'verdict', 'summary', 'findings'} or output['complete'] is not True:
        raise ReviewError('invalid review schema')
    if not isinstance(output['summary'], str) or not output['summary'].strip() or len(output['summary']) > 12000:
        raise ReviewError('invalid summary')
    if output['verdict'] not in RECOMMENDATIONS:
        raise ReviewError('invalid verdict')
    headings = re.findall(r'^## (.+)$', output['summary'], re.MULTILINE)
    if headings != list(REPORT_SECTIONS):
        raise ReviewError('missing or unordered report sections')
    for body in re.split(r'^## .+$', output['summary'], flags=re.MULTILINE)[1:]:
        if not re.search(r'[\u4e00-\u9fff]', body):
            raise ReviewError('report section must contain Chinese analysis')
    if not isinstance(output['findings'], list) or len(output['findings']) > 100:
        raise ReviewError('invalid findings')
    for finding in output['findings']:
        if not isinstance(finding, dict) or set(finding) != {'severity', 'file', 'line', 'title', 'evidence', 'suggestion'}:
            raise ReviewError('invalid finding schema')
        if finding['severity'] not in ('critical', 'high', 'medium', 'low', 'info'):
            raise ReviewError('invalid severity')
        for key in ('file', 'title', 'evidence', 'suggestion'):
            if not isinstance(finding[key], str) or not finding[key].strip() or len(finding[key]) > 12000:
                raise ReviewError('invalid finding text')
        if any(not re.search(r'[\u4e00-\u9fff]', finding[key]) for key in ('title', 'evidence', 'suggestion')):
            raise ReviewError('finding analysis must use Chinese')
        if type(finding['line']) is not int or finding['line'] < 1 or finding['line'] not in locations.get(finding['file'], set()):
            raise ReviewError('finding outside supplied diff')
    ordered = sorted(output['findings'], key=lambda f: list(PRIORITIES).index(f['severity']))
    output['findings'] = ordered
    if output['verdict'] == '通过' and any(f['severity'] in ('critical', 'high') for f in ordered):
        raise ReviewError('passing verdict contradicts blocking findings')
    risk = PRIORITIES[ordered[0]['severity']] if ordered else '未发现已证实缺陷；未验证风险见正文'
    output['summary'] = ('# Oasis 嵌入式代码评审报告\n\n## 评审结论\n' + output['verdict'] +
                         '\n最高已证实风险等级：' + risk + '\n\n' + SCOPE + '\n\n' + output['summary'] +
                         '\n\n## 最终合入建议\n' + RECOMMENDATIONS[output['verdict']])
    return output


def main():
    failure_code = 'CONFIG_INVALID'
    try:
        config = configuration(os.environ)
        failure_code = 'INPUT_INVALID'
        raw = sys.stdin.buffer.read(config['MAX_INPUT_BYTES'] + 1)
        if len(raw) > config['MAX_INPUT_BYTES']:
            raise ReviewError('input exceeds limit')
        value = decode(raw)
        lines, locations = validate_input(value)
        evidence = Evidence(lines)
        def expired(signum, frame):
            # SystemExit bypasses Hermes's broad Exception retry handlers.
            raise SystemExit(124)
        signal.signal(signal.SIGALRM, expired)
        signal.alarm(config['TIMEOUT_SECONDS'])
        failure_code = 'MODEL_EXECUTION_FAILED'
        result = run_agent(config, value, evidence)
        validate_completion(result)
        failure_code = 'OUTPUT_INVALID'
        output = validate_output(result, evidence, locations)
        signal.alarm(0)
        print(json.dumps(output, ensure_ascii=False))
        return 0
    except BaseException as error:
        signal.alarm(0)
        if isinstance(error, SystemExit) and error.code == 124:
            failure_code = 'REVIEW_TIMEOUT'
        elif isinstance(error, ReviewError) and str(error) == 'truncated or filtered response':
            failure_code = 'OUTPUT_TRUNCATED'
        # Never echo library exceptions, model output, endpoint, or input content.
        print(json.dumps({'complete': False, 'error_code': failure_code, 'summary': '评审执行失败，禁止合入。', 'findings': []}))
        return 1


if __name__ == '__main__':
    sys.exit(main())
