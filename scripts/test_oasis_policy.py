import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('adapter', Path(__file__).with_name('hermes_reviewer.py'))
adapter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(adapter)
from test_hermes_reviewer import DIFF

SECTIONS = ['评审范围', '修改目标与实现分析', '代码问题清单', '跨仓影响范围', '修改完整性评估', '历史问题回归评估', '构建与真机验证矩阵']
SUMMARY = '\n\n'.join('## '+name+'\n证据不足：仅提供本次提交日志及差异，未提供关联仓库。' for name in SECTIONS)

class OasisPolicyTests(unittest.TestCase):
 def validate(self, **updates):
  lines, locations = adapter.parse_diff(DIFF)
  evidence = adapter.Evidence(lines)
  evidence.dispatch('read_diff', {'start_line': 1, 'line_count': 200})
  output = dict(complete=True, verdict='证据不足', summary=SUMMARY, findings=[])
  output.update(updates)
  return adapter.validate_output({'completed': True, 'final_response':json.dumps(output)}, evidence, locations)
 def test_all_reviews_use_oasis_policy(self):
  self.assertIn('Oasis 智能眼镜', adapter.SYSTEM)
  self.assertIn('commit', adapter.SYSTEM)
  self.assertIn('推断', adapter.SYSTEM)
  self.assertIn('ANCS', adapter.SYSTEM)
 def test_insufficient_evidence_keeps_formal_report(self):
  result=self.validate()
  self.assertTrue(result['complete'])
  self.assertEqual(result['verdict'], '证据不足')
  self.assertIn('# Oasis 嵌入式代码评审报告', result['summary'])
  self.assertIn('## 最终合入建议\n拒绝合入', result['summary'])
 def test_missing_sections_rejected(self):
  with self.assertRaises(adapter.ReviewError): self.validate(summary='审查完成，没有问题')
 def test_unknown_verdict_rejected(self):
  with self.assertRaises(adapter.ReviewError): self.validate(verdict='随便合入')
 def test_commit_context_accepted(self):
  value=dict(repository='owner/repo',number=1,head_sha='a'*40,base_sha='b'*40,title='修复音频',diff=DIFF,
             description='',head_ref='fix/audio',base_ref='main',merge_base='c'*40,
             commits=[{'sha':'a'*40,'message':'fix: release audio focus after SCO disconnect'}])
  adapter.validate_input(value)
 def test_bad_commit_sha_rejected(self):
  value=dict(repository='owner/repo',number=1,head_sha='a'*40,base_sha='b'*40,title='修复',diff=DIFF,commits=[{'sha':'bad','message':'fix'}])
  with self.assertRaises(adapter.ReviewError):adapter.validate_input(value)

 def test_english_finding_details_rejected(self):
  with self.assertRaises(adapter.ReviewError):
   self.validate(verdict='不通过',findings=[dict(severity='high',file='a.py',line=1,title='Issue',evidence='x = 2',suggestion='Fix')])
