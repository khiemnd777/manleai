import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const expectedAssetHashes = {
  "apple-touch-icon.png": "2c52469fe6b8628eb6f6f6cddce9da5c371cd17410725e14d7578df176610f47",
  "favicon-32.png": "d8523af8fe1dbe687b4cc3500537c2a9eb564008e6ff76b2b035fdf0867175ee",
  "icon-192.png": "7445183d3133aa86f718256d70825d01bbb8225bfd76e6d642b62ca779052baf",
  "icon-512.png": "26de3011a45853ba0a5beede21c85fc06a953f9305407fa868c3ca9c090cb969",
  "tianna-ai-logo-720.png": "5ac065c87f3eddd2be5190bbd74d6e5f7e19b943d71f033be5587fdbfecc8591",
  "tianna-ai-logo.png": "a146da0a3365ee81c0d29c0784b129cdf9e916aaf30b017a251720eaa96df28d"
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
