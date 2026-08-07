import assert from "node:assert/strict";
import test from "node:test";
import { hostRoutingDecision, resolveIncomingHostname } from "./host-routing";

const decide=(hostname:string,pathname:string,production=true)=>hostRoutingDecision({hostname,pathname,production,marketingHost:"ai.knasoftware.com",salonHost:"salon.knasoftware.com"});
test("marketing and salon hosts own distinct route groups",()=>{
  assert.equal(decide("ai.knasoftware.com","/pricing"),"allow");
  assert.equal(decide("ai.knasoftware.com","/s/lotus"),"redirect_salon");
  assert.equal(decide("salon.knasoftware.com","/s/lotus"),"allow");
  assert.equal(decide("salon.knasoftware.com","/"),"rewrite_salon_home");
  assert.equal(decide("salon.knasoftware.com","/pricing"),"not_found");
  assert.equal(decide("platform.knasoftware.com","/s/lotus"),"not_found");
});
test("local development permits both route groups",()=>{assert.equal(decide("localhost","/pricing",false),"allow");assert.equal(decide("localhost","/s/lotus",false),"allow")});

test("standalone local production permits marketing and salon routes on the shared origin",()=>{
  const local=(pathname:string)=>hostRoutingDecision({hostname:"localhost",pathname,production:true,marketingHost:"localhost",salonHost:"localhost"});
  assert.equal(local("/"),"allow");
  assert.equal(local("/pricing"),"allow");
  assert.equal(local("/s/lotus"),"allow");
  assert.equal(hostRoutingDecision({hostname:"0.0.0.0",pathname:"/",production:true,marketingHost:"localhost",salonHost:"localhost"}),"not_found");
  assert.equal(decide("localhost","/pricing",true),"not_found");
});

test("incoming host parser strips ports and rejects URL-shaped or ambiguous values",()=>{
  assert.equal(resolveIncomingHostname("localhost:3090","0.0.0.0"),"localhost");
  assert.equal(resolveIncomingHostname("AI.KNASOFTWARE.COM:443","0.0.0.0"),"ai.knasoftware.com");
  assert.equal(resolveIncomingHostname("[::1]:3090","0.0.0.0"),"::1");
  assert.equal(resolveIncomingHostname(null,"localhost"),"localhost");
  assert.equal(resolveIncomingHostname("https://ai.knasoftware.com","localhost"),"");
  assert.equal(resolveIncomingHostname("ai.knasoftware.com,evil.example","localhost"),"");
  assert.equal(resolveIncomingHostname("user@ai.knasoftware.com","localhost"),"");
  assert.equal(resolveIncomingHostname("ai.knasoftware.com/pricing","localhost"),"");
});
