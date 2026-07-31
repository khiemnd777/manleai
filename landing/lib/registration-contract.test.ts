import assert from "node:assert/strict";
import test from "node:test";
import { buildRegistrationSubmission, CONTACT_CONSENT_VERSION, type RegistrationFormFields } from "./registration-contract";

test("public registration submits the approved versioned contact-consent contract",()=>{
  assert.equal(CONTACT_CONSENT_VERSION,"tenant-registration-contact-v1");
});

const fields:RegistrationFormFields={contact_full_name:"QA Owner",contact_email:"qa@example.test",contact_phone:"3125550148",salon_name:"QA Salon",salon_phone:"7735550180",city:"Chicago",state:"IL",zip_code:"60614",salon_website:"",location_count:1,preferred_contact_language:"en",current_booking_system:"",estimated_weekly_call_volume:"",requested_help:"",notes:"",contact_consent:true,website_confirmation:""};

test("registration payload maps stable source, locale, plan and submission key",()=>{
  const submissionKey="249705ef-25f0-4b5c-9247-883f10e504ca";
  const first=buildRegistrationSubmission(fields,{submissionKey,locale:"vi",source:"pricing",plan:"growth"});
  const retry=buildRegistrationSubmission({...fields,notes:"preserved after a network error"},{submissionKey,locale:"vi",source:"pricing",plan:"growth"});
  assert.equal(first.submission_key,submissionKey);
  assert.equal(retry.submission_key,submissionKey);
  assert.equal(first.locale,"vi");assert.equal(first.source_page,"pricing");assert.equal(first.marketing_plan_interest,"growth");
  assert.equal(first.consent_version,CONTACT_CONSENT_VERSION);
});
