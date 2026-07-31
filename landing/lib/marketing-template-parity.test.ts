import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const expectedAssetHashes = {
  "apple-touch-icon.png": "bc219f5ec28359a8d3f1b47cd62ad6c8b07bf7ccb91a0c4722c89ff9867b3149",
  "favicon-32.png": "884f41f6893a2e0f10b0ef298d4226b5be6b289f467a5a9a748c3db271e7e72a",
  "icon-192.png": "fcd4a4be3cf4b6838cb0742755bc560b8d8bfa3c0230c4cfd48f861b7b4175aa",
  "icon-512.png": "de8d4a17498c8781bfacc8dc2045fb46a6af922ee22333ba90b623eb7bbb2efc",
  "manle-ai-logo-720.png": "083191af9bba51437d7d1309037a4e26c787e06c2d79af0ee567faaf11cce297",
  "manle-ai-logo.png": "c44579e7ca7525cb643418defef9fbfa2bb68cbd9c45bd51ffd5fa2be5ca1182"
} as const;

test("marketing brand assets retain the approved template bytes",()=>{
  for(const [name,expected] of Object.entries(expectedAssetHashes)){
    const bytes=readFileSync(join(process.cwd(),"public","brand",name));
    assert.equal(createHash("sha256").update(bytes).digest("hex"),expected,name);
  }
});

test("marketing stylesheet retains the template's core visual contract",()=>{
  const css=readFileSync(join(process.cwd(),"components","marketing","marketing.module.css"),"utf8");
  for(const evidence of [
    "--blue: #0757c7",
    "--red: #ff2a2f",
    "--container: 1180px",
    ".hero-logo",
    ".feature-bento",
    ".phone-shell",
    ".integration-map",
    ".final-cta",
    "@media (max-width: 760px)"
  ]) assert.match(css,new RegExp(evidence.replace(/[.*+?^${}()|[\]\\]/g,"\\$&")),evidence);
});

test("responsive marketing navigation and pricing keep mobile content in normal flow",()=>{
  const css=readFileSync(join(process.cwd(),"components","marketing","marketing.module.css"),"utf8");
  const marketing=readFileSync(join(process.cwd(),"components","marketing","marketing-site.tsx"),"utf8");
  const pricing=readFileSync(join(process.cwd(),"components","marketing","pricing-page.tsx"),"utf8");

  assert.match(css,/\.mobile-nav-language\s*\{/);
  assert.match(css,/\.price-card \.full-button\s*\{\s*margin-top:24px;/);
  assert.match(css,/\.comparison-mobile\s*\{\s*display:grid;/);
  assert.doesNotMatch(css,/top:\s*330px/);
  assert.match(marketing,/mobile-nav-language/);
  assert.match(pricing,/mobile-nav-language/);
  assert.match(pricing,/comparison-mobile/);
  assert.ok((pricing.match(/comparisonRows\.map/g)??[]).length>=2);
});
