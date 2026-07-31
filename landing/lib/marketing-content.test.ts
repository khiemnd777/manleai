import assert from "node:assert/strict";
import test from "node:test";
import { marketingContent } from "./marketing-content";

test("English and Vietnamese website translations include every required section",()=>{
  for(const locale of ["en","vi"] as const){
    const content=marketingContent[locale];
    assert.ok(content.hero.title&&content.hero.lead&&content.hero.note);
    assert.equal(content.hero.trust.length,3);
    assert.equal(content.quickValues.length,3);
    assert.equal(content.problems.items.length,3);
    assert.ok(content.features.items.length>=5);
    assert.equal(content.workflow.steps.length,4);
    assert.ok(content.simulation.lines.length>=4);
    assert.equal(content.simulation.benefits.length,3);
    assert.ok(content.outcomes.items.length>=4);
    assert.ok(content.integration.title&&content.integration.body);
    assert.ok(content.faq.items.length>=5);
    assert.ok(content.final.title&&content.final.cta);
  }
});

test("launch voice claims are English-only while owner-facing website translations remain available",()=>{
  assert.equal(marketingContent.en.hero.trust[0],"English call handling");
  assert.equal(marketingContent.vi.hero.trust[0],"Cuộc gọi tiếng Anh");
  assert.match(marketingContent.en.features.items[0].title,/English conversations/i);
  assert.match(marketingContent.vi.features.items[0].title,/tiếng Anh/i);
  assert.equal(marketingContent.en.outcomes.items[1].metric,"ENGLISH");
  assert.equal(marketingContent.vi.outcomes.items[1].metric,"ENGLISH");
  assert.match(marketingContent.en.faq.items[0].answer,/marketing site and onboarding contact.*English and Vietnamese/i);
  assert.match(marketingContent.vi.faq.items[0].answer,/Website marketing.*tiếng Anh và tiếng Việt/i);
  assert.doesNotMatch(marketingContent.en.hero.lead,/Vietnamese|bilingual/i);
  assert.doesNotMatch(marketingContent.vi.hero.lead,/tiếng Việt|song ngữ/i);
  assert.ok(marketingContent.vi.simulation.lines.every(({text})=>/[A-Za-z]/.test(text)));
});

test("marketing copy preserves request-only and conditional-confirmation boundaries",()=>{
  assert.match(marketingContent.en.hero.note,/pending for owner review/i);
  assert.match(marketingContent.en.faq.items.map(({question,answer})=>`${question} ${answer}`).join(" "),/durable evidence/i);
  assert.match(marketingContent.vi.hero.note,/chờ chủ tiệm review/i);
  assert.match(marketingContent.vi.faq.items.map(({question,answer})=>`${question} ${answer}`).join(" "),/evidence bền vững/i);
});
