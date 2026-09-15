"""Boundary tests use a fake Hermes API; never contact a model or run PR code."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('hermes_reviewer.py')
DIFF = 'diff --git a/a.py b/a.py\n--- a/a.py\n+++ b/a.py\n@@ -1 +1 @@\n-x = 1\n+x = 2\n'
GOOD = {'complete': True, 'summary': 'Reviewed changes.', 'findings': []}
FAKE = '''import os, json
print('SECRET FROM LIBRARY')
from hermes_cli.env_loader import load_hermes_dotenv
assert load_hermes_dotenv() == []
assert 'GITEA_TOKEN' not in os.environ
assert 'FEISHU_APP_SECRET' not in os.environ
assert os.getcwd() == os.environ['HOME']
assert os.environ['HERMES_HOME'].startswith(os.environ['HOME'])
assert os.environ['HERMES_MANAGED_DIR'].startswith(os.environ['HOME'])
class AIAgent:
 def __init__(self, **kw):
  assert kw['enabled_toolsets'] == []
  assert kw['skip_memory'] and kw['skip_context_files']
  assert not kw['load_soul_identity'] and not kw['save_trajectories']
  assert kw['max_tokens'] == 8192
  assert kw['api_key'] == 'test-key' and kw['model'] == 'test-model'
  self.tools = []
  self.valid_tool_names = set()
 def run_conversation(self, **kw):
  assert self.valid_tool_names == {'read_diff'}
  assert 'SECRET' not in kw['user_message']
  assert json.loads(handle_function_call('terminal', {'command':'id'}))['error']
  if READ:
   data = json.loads(handle_function_call('read_diff', {'start_line':1, 'line_count':200}))
   assert '+x = 2' in data['text']
  if RESULT == 'raise':
   raise RuntimeError('PRIVATE FAILURE DETAILS')
  if RESULT == 'sleep':
   import time
   time.sleep(10)
  return RESULT
'''

class AdapterTests(unittest.TestCase):
 def run_adapter(self, result=None, payload=None, read=True, extra=None, raw=None):
  with tempfile.TemporaryDirectory() as td:
   root = Path(td)
   (root / 'run_agent.py').write_text('READ = '+repr(read)+'\nRESULT = '+repr(result if result is not None else {'completed':True, 'final_response':json.dumps(GOOD)})+'\n'+FAKE)
   # The isolation shim must block this source's dotenv and external secret loading.
   (root / 'hermes_cli').mkdir()
   (root / 'hermes_cli/__init__.py').write_text('')
   (root / 'hermes_cli/env_loader.py').write_text('def load_hermes_dotenv(**kwargs):\n raise AssertionError("dotenv must be disabled")\n')
   env = dict(os.environ, REVIEW_HERMES_PATH=td, REVIEW_MODEL='test-model', REVIEW_BASE_URL='https://model.example/v1', REVIEW_API_KEY='test-key', GITEA_TOKEN='PRIVATE', FEISHU_APP_SECRET='PRIVATE')
   if extra: env.update(extra)
   value = payload or {'repository':'owner/repo','number':1,'head_sha':'a'*40,'base_sha':'b'*40,'title':'change','diff':DIFF}
   return subprocess.run([sys.executable, '-I', str(SCRIPT)], input=raw if raw is not None else json.dumps(value), capture_output=True, text=True, env=env, timeout=10)
 def test_success_isolated_exact_json(self):
  p=self.run_adapter(); self.assertEqual(p.returncode,0,p.stderr)
  data=json.loads(p.stdout); self.assertTrue(data['complete']); self.assertIn('diff-only', data['summary']); self.assertEqual(p.stderr,'')
 def test_incomplete_agent_fails(self):
  p=self.run_adapter({'completed':False,'final_response':json.dumps(GOOD)}); self.assertNotEqual(p.returncode,0); self.assertFalse(json.loads(p.stdout)['complete'])
 def test_agent_error_fails_even_with_complete(self):
  self.assertNotEqual(self.run_adapter({'completed':True,'error':'PRIVATE','final_response':json.dumps(GOOD)}).returncode,0)
 def test_unread_diff_fails(self):
  self.assertNotEqual(self.run_adapter(read=False).returncode,0)
 def test_malformed_output_fails(self):
  for value in ['not json','```json\n'+json.dumps(GOOD)+'\n```', json.dumps({'complete':'true','summary':'x','findings':[]}), json.dumps({'complete':True,'findings':[]}), json.dumps(dict(GOOD,complete=False)),json.dumps(dict(GOOD,findings=[{'severity':'unknown'}]))]:
   with self.subTest(value=value): self.assertNotEqual(self.run_adapter({'completed':True,'final_response':value}).returncode,0)
 def test_invalid_findings_fail(self):
  good={'severity':'high','file':'a.py','line':1,'title':'Issue','evidence':'x = 2','suggestion':'Fix'}
  for change in [{'severity':'urgent'},{'line':True},{'line':100},{'file':'../../secret'},{'evidence':''}]:
   output=dict(GOOD,findings=[dict(good,**change)])
   with self.subTest(change=change): self.assertNotEqual(self.run_adapter({'completed':True,'final_response':json.dumps(output)}).returncode,0)
 def test_valid_finding_passes(self):
  output=dict(GOOD,findings=[{'severity':'high','file':'a.py','line':1,'title':'Issue','evidence':'x = 2','suggestion':'Fix'}])
  self.assertEqual(self.run_adapter({'completed':True,'final_response':json.dumps(output)}).returncode,0)
 def test_oversized_input_fails(self):
  self.assertNotEqual(self.run_adapter(raw=' '*524289).returncode,0)
 def test_duplicate_json_keys_fail(self):
  self.assertNotEqual(self.run_adapter(raw='{"number":1,"number":2}').returncode,0)
 def test_missing_configuration_fails_without_secret(self):
  p=self.run_adapter(extra={'REVIEW_API_KEY':''}); self.assertNotEqual(p.returncode,0); self.assertNotIn('PRIVATE',p.stdout+p.stderr)
 def test_truncated_or_binary_diff_fails(self):
  for diff in [DIFF.replace('@@ -1 +1 @@','@@ -1,2 +1,2 @@'), 'Binary files a/a and b/a differ\n', DIFF+'[truncated]\n']:
   payload={'repository':'owner/repo','number':1,'head_sha':'a'*40,'base_sha':'b'*40,'title':'change','diff':diff}
   with self.subTest(diff=diff): self.assertNotEqual(self.run_adapter(payload=payload).returncode,0)
 def test_library_exception_does_not_leak_diagnostics(self):
  p=self.run_adapter('raise'); self.assertNotEqual(p.returncode,0)
  self.assertFalse(json.loads(p.stdout)['complete']); self.assertEqual(p.stderr,'')
  self.assertNotIn('SECRET',p.stdout); self.assertNotIn('PRIVATE',p.stdout)
 def test_deadline_fails_closed(self):
  p=self.run_adapter('sleep',extra={'REVIEW_TIMEOUT_SECONDS':'1'})
  self.assertNotEqual(p.returncode,0); self.assertFalse(json.loads(p.stdout)['complete'])
 def test_oversized_output_fails(self):
  self.assertNotEqual(self.run_adapter({'completed':True,'final_response':'x'*131073}).returncode,0)
 def test_invalid_limits_fail(self):
  for limits in [{'REVIEW_MAX_TOKENS':'0'},{'REVIEW_MAX_ITERATIONS':'1000'},{'REVIEW_MAX_INPUT_BYTES':'99999999'}]:
   with self.subTest(limits=limits): self.assertNotEqual(self.run_adapter(extra=limits).returncode,0)
 def test_complete_multiple_files_pass(self):
  payload={'repository':'owner/repo','number':1,'head_sha':'a'*40,'base_sha':'b'*40,'title':'change','diff':DIFF+DIFF.replace('a.py','b.py')}
  p=self.run_adapter(payload=payload); self.assertEqual(p.returncode,0,p.stdout)
 def test_truncated_trailing_file_fails(self):
  for tail in ['diff --git a/security.py b/security.py\n', 'diff --git a/security.py b/security.py\n--- a/security.py\n+++ b/security.py\n', '--- a/security.py\n+++ b/security.py\n']:
   payload={'repository':'owner/repo','number':1,'head_sha':'a'*40,'base_sha':'b'*40,'title':'change','diff':DIFF+tail}
   with self.subTest(tail=tail):
    p=self.run_adapter(payload=payload)
    self.assertNotEqual(p.returncode,0); self.assertFalse(json.loads(p.stdout)['complete'])
 def test_length_finish_reason_fails(self):
  self.assertNotEqual(self.run_adapter({'completed':True,'final_response':json.dumps(GOOD),'messages':[{'role':'assistant','finish_reason':'length'}]}).returncode,0)



@unittest.skipUnless(os.environ.get('TEST_HERMES_SOURCE'), 'set TEST_HERMES_SOURCE for installed Hermes integration')
class InstalledHermesTests(unittest.TestCase):
 def test_real_agent_reads_diff_through_local_model_server(self):
  from http.server import HTTPServer, BaseHTTPRequestHandler
  import threading
  requests = []
  class Handler(BaseHTTPRequestHandler):
   def log_message(self, *args): pass
   def do_GET(self):
    self.send_response(200); self.end_headers(); self.wfile.write(b'{"data":[]}')
   def do_POST(self):
    data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
    if self.path == '/api/show':
     self.send_response(404); self.end_headers(); return
    requests.append(data)
    used = any(m.get('role') == 'tool' for m in data.get('messages', []))
    if used:
     message = {'role': 'assistant', 'content': json.dumps(GOOD)}
    else:
     message = {'role': 'assistant', 'content': None, 'tool_calls': [{'index': 0, 'id': 'call_diff', 'type': 'function', 'function': {'name': 'read_diff', 'arguments': json.dumps({'start_line': 1, 'line_count': 200})}}]}
    response = {'id': 'chatcmpl-test', 'object': 'chat.completion.chunk', 'created': 1, 'model': 'test-model', 'choices': [{'index': 0, 'delta': message, 'finish_reason': None}], 'usage': {'prompt_tokens': 100, 'completion_tokens': 100, 'total_tokens': 200}}
    final = dict(response, choices=[{'index': 0, 'delta': {}, 'finish_reason': 'stop' if used else 'tool_calls'}])
    self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.end_headers()
    self.wfile.write(('data: '+json.dumps(response)+'\n\ndata: '+json.dumps(final)+'\n\ndata: [DONE]\n\n').encode())
  server = HTTPServer(('127.0.0.1', 0), Handler)
  thread = threading.Thread(target=server.serve_forever, daemon=True)
  thread.start()
  try:
   env = dict(os.environ, REVIEW_HERMES_PATH=os.environ['TEST_HERMES_SOURCE'], REVIEW_MODEL='test-model', REVIEW_BASE_URL='http://127.0.0.1:'+str(server.server_port)+'/v1', REVIEW_API_KEY='dummy-only', REVIEW_TIMEOUT_SECONDS='60')
   payload = {'repository':'test/repo','number':1,'head_sha':'a'*40,'base_sha':'b'*40,'title':'synthetic fixture','diff':DIFF}
   p = subprocess.run([sys.executable, '-I', str(SCRIPT)], input=json.dumps(payload), text=True, capture_output=True, env=env, timeout=75)
   self.assertEqual(p.returncode, 0, p.stdout+p.stderr)
   self.assertTrue(json.loads(p.stdout)['complete'])
   self.assertEqual(p.stderr, '')
   self.assertGreaterEqual(len(requests), 2)
   for request in requests:
    self.assertEqual([t['function']['name'] for t in request.get('tools', [])], ['read_diff'])
   evidence = [m['content'] for request in requests for m in request['messages'] if m.get('role') == 'tool']
   self.assertTrue(any('+x = 2' in str(content) for content in evidence))
  finally:
   server.shutdown(); server.server_close(); thread.join()

if __name__ == '__main__': unittest.main()
