package conversationeval

import (
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

// DefaultRealSalonCorpus is the authored source used to produce the retained
// JSON artifact. The caller language below was written journey by journey;
// there is no phrase mutation, cartesian expansion, or paraphrase generator.
func DefaultRealSalonCorpus() RealSalonCorpus {
	journeys := []RealSalonJourney{
		// 15 service advice and discovery journeys.
		realJourney("advice-001", "service_advice_discovery", "Unsure what to book, then says nails are too long", conversation.ChannelSimulator, "lotus", true, RealSalonInitialState{}, "I don't know what service I should book for my nails.", "My nails are too long and I mainly want them shorter.", "They are natural nails, and I would prefer something easy to maintain.", "Which service in the salon's list best matches those needs?"),
		realJourney("advice-002", "service_advice_discovery", "Asks broadly for a nail service", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want a service for my nails, but I am not sure which one.", "They keep breaking at the corners.", "I want to keep them short and natural for work."),
		realJourney("advice-003", "service_advice_discovery", "Wedding guest wants an appropriate service", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I have a wedding to attend and need help choosing a nail service.", "I want an elegant glossy look, not extensions.", "The wedding is in two weeks, so I need it to last.", "Please recommend only a service that is actually in the salon catalog."),
		realJourney("advice-004", "service_advice_discovery", "First salon visit for basic cleanup", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "This is my first nail salon visit. What should I get?", "I mostly need the shape and cuticles cleaned up.", "No color this time; I want a neat natural finish."),
		realJourney("advice-005", "service_advice_discovery", "Nail biter wants a natural improvement", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I bite my nails and do not know what appointment makes sense.", "I just want them to look healthier and more even.", "Please keep the result natural and not too long."),
		realJourney("advice-006", "service_advice_discovery", "Office worker wants chip resistance", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Can you suggest something practical for office nails?", "Regular polish chips on me after a couple of days.", "I like short, subtle nails and do not want extensions."),
		realJourney("advice-007", "service_advice_discovery", "Vacation foot-care choice", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I am going on vacation and need help choosing something for my toes.", "I want the color to hold up at the beach.", "My toenails also need trimming before I leave."),
		realJourney("advice-008", "service_advice_discovery", "Busy parent prioritizes low maintenance", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "I have very little time for upkeep. What nail service would you suggest?", "It is for my hands, and my nails are natural.", "I care more about low maintenance than having a fancy finish."),
		realJourney("advice-009", "service_advice_discovery", "Gentle grooming for an older parent", conversation.ChannelPhone, "harbor", false, RealSalonInitialState{}, "I am bringing my older mother and need a gentle basic nail service for her.", "She only needs careful trimming and cleanup.", "She does not want polish or anything elaborate."),
		realJourney("advice-010", "service_advice_discovery", "Teen wants a school-dance service", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "My daughter has a school dance. Which service should we look at?", "Her nails are short, and she wants a simple color.", "We would like to keep the visit fairly quick."),
		realJourney("advice-011", "service_advice_discovery", "Guitar player needs short protected nails", conversation.ChannelPhone, "willow", false, RealSalonInitialState{}, "I play guitar and need advice on what to book for my hands.", "The nails must stay short, but they split easily.", "A clear or natural-looking protective finish would be best."),
		realJourney("advice-012", "service_advice_discovery", "Gardener needs cleanup without extensions", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{}, "My hands are rough from gardening. What kind of appointment should I choose?", "The cuticles need attention and the nails need reshaping.", "I do not want extensions or bright color."),
		realJourney("advice-013", "service_advice_discovery", "Customer wants longer-wearing polish", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "My polish always chips, and I do not know which service would last longer.", "These are my natural nails.", "I want color but no added length."),
		realJourney("advice-014", "service_advice_discovery", "Men's tidy hand grooming", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{}, "I only want my hands to look tidy. What should I book?", "Please trim and shape the nails and clean around them.", "I do not need polish."),
		realJourney("advice-015", "service_advice_discovery", "Runner asks about long toenails", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I run a lot and my toenails have gotten too long. What service fits that?", "I mainly need ordinary trimming and basic foot care.", "I am not looking for nail art or extensions."),

		// 15 consultation and recommendation journeys.
		realJourney("consult-001", "service_consultation_recommendation", "Compare dip and acrylic for strength", conversation.ChannelSimulator, "lotus", true, RealSalonInitialState{}, "Should I choose dip powder or acrylic if I want stronger nails?", "My nails are natural but thin, and I do not need extra length.", "Durability matters more to me than the lowest price.", "I definitely do not want extensions, so which catalog service fits?"),
		realJourney("consult-002", "service_consultation_recommendation", "Compare gel with classic manicure", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "What is the better fit for me, a gel manicure or a classic manicure?", "I want color that lasts about two weeks.", "I am okay with removal later if that is needed."),
		realJourney("consult-003", "service_consultation_recommendation", "Existing gel needs removal and refresh", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I already have gel on. What should I book to change the color?", "The current gel is three weeks old with no lifting.", "I want another gel color after it is removed.", "Please tell me whether that means one service or two separate catalog services."),
		realJourney("consult-004", "service_consultation_recommendation", "Customer asks fill versus full set", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "How do I know whether I need an acrylic fill or a new full set?", "The set is about four weeks old and two nails are lifting.", "I would like to keep roughly the same length."),
		realJourney("consult-005", "service_consultation_recommendation", "Natural nails with modest added length", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want a little more length but do not know which service to choose.", "My natural nails are short and healthy.", "I want a medium almond shape, not extremely long nails."),
		realJourney("consult-006", "service_consultation_recommendation", "Thin damaged nails need catalog-backed advice", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "My nails feel thin after removing product. Can you help me choose a service?", "There is no pain or broken skin; they just bend easily.", "I want a gentle, natural-looking option."),
		realJourney("consult-007", "service_consultation_recommendation", "Customer asks for chrome finish", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want a chrome finish. What base service should I book with it?", "It is for my natural fingernails.", "I would like the finish to last longer than regular polish."),
		realJourney("consult-008", "service_consultation_recommendation", "French finish with natural length", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "Which service should I get for a simple French look?", "I want to keep my natural length.", "A glossy finish and good durability are my priorities."),
		realJourney("consult-009", "service_consultation_recommendation", "Nail art requires a base service", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want nail art, but I do not know what main service goes with it.", "My nails are natural and short.", "I want a gel-color base with art on two fingers."),
		realJourney("consult-010", "service_consultation_recommendation", "Minimal-product preference without medical claims", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{}, "I prefer very few products and want help choosing a simple service.", "The simplest grooming option is what I have in mind.", "Basic shaping and a natural finish would be enough."),
		realJourney("consult-011", "service_consultation_recommendation", "Elegant result on very short nails", conversation.ChannelPhone, "willow", false, RealSalonInitialState{}, "Can short nails still look elegant, and what should I book?", "I want to keep them short.", "A neutral long-wearing color would be ideal."),
		realJourney("consult-012", "service_consultation_recommendation", "Hands-on job prioritizes durability", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I work with my hands all day. Which service is most practical?", "My nails are natural and regular polish chips quickly.", "Please prioritize durability without adding length."),
		realJourney("consult-013", "service_consultation_recommendation", "Recommendation constrained by cost", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need help choosing, but I want the lower-cost option.", "I only need basic care and a fresh color.", "It is okay if it does not last as long as gel."),
		realJourney("consult-014", "service_consultation_recommendation", "Recommendation constrained by visit duration", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "What should I choose if I need to be done fairly quickly?", "It is just for my natural fingernails.", "I need shaping, cleanup, and a simple color."),
		realJourney("consult-015", "service_consultation_recommendation", "Customer asks about maintenance commitment", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "Help me choose something that does not need frequent salon maintenance.", "I want my natural nails to look polished.", "I can come back for removal, but I do not want extension fills."),

		// 10 catalog and operating questions.
		realJourney("question-001", "catalog_and_salon_questions", "Customer asks to see services before choosing", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Show me your services.", "Which of those are for natural fingernails?", "I want help picking between the natural-nail choices.", "Please compare the lower-cost and more durable choices without inventing details."),
		realJourney("question-002", "catalog_and_salon_questions", "Price question without invented price data", conversation.ChannelPhone, "willow", false, RealSalonInitialState{}, "How much are your manicure services?", "I am asking about gel rather than regular polish.", "If the exact price is unavailable, please tell me how I can verify it."),
		realJourney("question-003", "catalog_and_salon_questions", "Business-hours question then booking", conversation.ChannelPhone, "lotus", true, RealSalonInitialState{}, "What time do you close on Saturday?", "Great, do you offer gel manicures too?", "I would like to book one for 2027-03-13."),
		realJourney("question-004", "catalog_and_salon_questions", "Walk-in policy question", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{}, "Do you take walk-ins?", "If you cannot guarantee that, can I make an appointment instead?", "I need basic hand care next Tuesday."),
		realJourney("question-005", "catalog_and_salon_questions", "Staff question remains catalog grounded", conversation.ChannelPhone, "lotus", true, RealSalonInitialState{}, "Who can do a gel manicure?", "I do not have a preferred technician.", "Can anyone available see me on 2027-03-15?"),
		realJourney("question-006", "catalog_and_salon_questions", "Check whether a pedicure exists", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "Do you have a pedicure service?", "What is the foot-care option called?", "I only need something simple for my toes."),
		realJourney("question-007", "catalog_and_salon_questions", "Ask whether removal is offered", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "Can you remove old gel?", "Can removal be followed by a new manicure?", "I want new gel after the old product comes off."),
		realJourney("question-008", "catalog_and_salon_questions", "Ask for catalog-backed comparison", conversation.ChannelSimulator, "lotus", true, RealSalonInitialState{}, "What is the difference between your classic and gel manicure?", "Which one is described as more durable?", "I want the durable choice for natural nails."),
		realJourney("question-009", "catalog_and_salon_questions", "Ask how many manicure choices exist", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "How many manicure choices do you have?", "Please name the natural-nail options.", "I need help deciding after hearing the list."),
		realJourney("question-010", "catalog_and_salon_questions", "Cancellation-policy question before booking", conversation.ChannelSimulator, "willow", true, RealSalonInitialState{}, "What is your cancellation policy?", "I understand if the exact policy needs owner confirmation.", "Can I still start a booking for a manicure?"),

		// 20 ordinary single-customer booking journeys. Each stops before confirmation.
		realJourney("booking-001", "single_customer_booking", "Book gel manicure with open staff preference", conversation.ChannelPhone, "lotus", true, RealSalonInitialState{}, "I would like to book a gel manicure.", "Next Friday, 2027-03-12, works for me.", "Any technician is fine.", "The 2 PM option works.", "The appointment name is Jordan Lee.", "My callback number is 312-555-0188."),
		realJourney("booking-002", "single_customer_booking", "Book classic manicure in the morning", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Can I book a classic manicure?", "I need 2027-03-16.", "Morning is best, and I do not have a staff preference.", "The 10 AM opening works for me.", "My name is Taylor Brooks."),
		realJourney("booking-003", "single_customer_booking", "Book spa pedicure after work", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need an appointment for a spa pedicure.", "Please check 2027-03-17.", "Anything after 5 PM with anyone available.", "If nothing fits that window, explain that clearly without changing my requested date."),
		realJourney("booking-004", "single_customer_booking", "Book protective manicure by alias", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "I want to book the strong gel hands service.", "How about 2027-03-18?", "Around noon with any available technician."),
		realJourney("booking-005", "single_customer_booking", "Book simple care at small catalog salon", conversation.ChannelPhone, "harbor", false, RealSalonInitialState{}, "Please book the quiet reset service for me.", "I can come on 2027-03-19.", "Ten in the morning would be good."),
		realJourney("booking-006", "single_customer_booking", "Book dip manicure with named staff", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I want a dip powder manicure.", "Check 2027-03-20 for me.", "I would prefer Maya in the afternoon."),
		realJourney("booking-007", "single_customer_booking", "Book gel removal before service decision", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need gel removal and another manicure.", "The new manicure should be gel too.", "I am free on 2027-03-22 after 1 PM."),
		realJourney("booking-008", "single_customer_booking", "Book acrylic full set", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Can I schedule an acrylic full set?", "I want it on 2027-03-23.", "Any technician around 11 AM is okay."),
		realJourney("booking-009", "single_customer_booking", "Book manicure plus nail art", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want a gel manicure with nail art.", "Two accent nails are enough.", "Please look at 2027-03-24 in the afternoon."),
		realJourney("booking-010", "single_customer_booking", "Book natural manicure with no polish", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "I want to schedule a basic manicure without polish.", "I can come 2027-03-25.", "The earliest opening with anyone is fine."),
		realJourney("booking-011", "single_customer_booking", "Book pedicure using category language", conversation.ChannelPhone, "willow", false, RealSalonInitialState{}, "I need to book something for my feet.", "The simple foot-care service is what I want.", "Saturday 2027-03-27 around 3 PM would work."),
		realJourney("booking-012", "single_customer_booking", "Book service after asking for recommendation", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I want a durable manicure for natural nails and then book it.", "Gel sounds right.", "Please check 2027-03-29 after 4 PM."),
		realJourney("booking-013", "single_customer_booking", "Book two services for one customer", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need a gel manicure and a spa pedicure for myself.", "I would like both on 2027-03-30.", "Any staff combination around midday is fine."),
		realJourney("booking-014", "single_customer_booking", "Book repair plus manicure", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "One nail is broken, and I also need a manicure.", "Please include nail repair with a classic manicure.", "I am available 2027-03-31 in the morning."),
		realJourney("booking-015", "single_customer_booking", "Book with technician preference then allow anyone", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want to book a gel manicure with Maya.", "Try 2027-04-01 after 2 PM.", "If Maya is unavailable, anyone qualified is okay."),
		realJourney("booking-016", "single_customer_booking", "Book using service alias", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I would like the no-chip hands service.", "Please check 2027-04-02.", "Late morning with any technician is best."),
		realJourney("booking-017", "single_customer_booking", "Book appointment and provide name", conversation.ChannelPhone, "willow", false, RealSalonInitialState{}, "Can you start a gel manicure booking?", "I want 2027-04-03 around 1 PM.", "The appointment name is Jordan Lee."),
		realJourney("booking-018", "single_customer_booking", "Book appointment and provide phone", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{HasCustomerName: true}, "I need a classic manicure on 2027-04-05.", "Any opening after 10 AM works.", "My callback number is 312-555-0188."),
		realJourney("booking-019", "single_customer_booking", "Book at exact requested time", conversation.ChannelPhone, "harbor", false, RealSalonInitialState{}, "I want to schedule quiet care.", "Can you check 2027-04-06 exactly at 3 PM?", "Sam is fine if that time is available."),
		realJourney("booking-020", "single_customer_booking", "Book after catalog list", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Tell me the manicure choices, then help me book one.", "I choose the classic manicure.", "I would like 2027-04-07 in the afternoon."),

		// 10 party and group booking journeys.
		realJourney("party-001", "party_group_booking", "Three friends need mixed services", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need appointments for three friends together.", "Two want gel manicures and one wants a spa pedicure.", "We are looking at 2027-04-09 around 2 PM.", "Staggered starts are okay if there is no common time."),
		realJourney("party-002", "party_group_booking", "Parent and child book different services", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "I want to book for me and my daughter.", "I need a gel manicure, and she needs a classic manicure.", "Saturday 2027-04-10 in the morning is best.", "We can use different technicians but would like nearby times."),
		realJourney("party-003", "party_group_booking", "Four-person pedicure group", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "Can you help with a group of four for pedicures?", "All four want the spa pedicure.", "We would prefer 2027-04-12 after 3 PM."),
		realJourney("party-004", "party_group_booking", "Bridal group asks about staggered times", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I am planning nails for a bridal group of five.", "Everyone wants gel manicures, and two also want nail art.", "Could you check 2027-04-13 even if the starts need to be staggered?"),
		realJourney("party-005", "party_group_booking", "Couple wants hand and foot services", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need to book for two people.", "We both want pedicures, and one of us also wants a classic manicure.", "We are free 2027-04-14 around noon."),
		realJourney("party-006", "party_group_booking", "Three guests need category clarification", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "There are three of us, and we all want our nails done.", "Two need hand services and one needs foot care.", "Please help us choose the specific services before checking 2027-04-15."),
		realJourney("party-007", "party_group_booking", "Group has individual technician preference", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I am booking for three family members.", "All need classic manicures, but one person prefers Maya.", "We want 2027-04-16 in the late morning."),
		realJourney("party-008", "party_group_booking", "Group accepts split availability", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Can three of us get gel manicures on the same day?", "We do not have to start at exactly the same time.", "Check 2027-04-17 after 1 PM."),
		realJourney("party-009", "party_group_booking", "Two customers need removal plus new gel", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need appointments for two people with old gel.", "Both of us need removal and new gel manicures.", "We can come 2027-04-19 in the morning."),
		realJourney("party-010", "party_group_booking", "Six-person request should remain bounded", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I am calling for a group of six.", "Four want manicures and two want pedicures.", "We are considering 2027-04-20 and can accept split times."),

		// 10 reschedule and cancel journeys.
		realJourney("change-001", "reschedule_cancel", "Reschedule by date and service context", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionReschedule}, "I need to move my gel manicure appointment.", "It is currently under Jordan Lee for next Friday.", "Please look for the following Monday afternoon instead.", "Keep the same service and do not cancel the old time unless the new one succeeds."),
		realJourney("change-002", "reschedule_cancel", "Reschedule when customer has multiple appointments", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionReschedule}, "Can you reschedule one of my appointments?", "I mean the pedicure, not the manicure.", "Move it to 2027-04-22 after 4 PM if possible."),
		realJourney("change-003", "reschedule_cancel", "Move appointment to earlier time same day", conversation.ChannelPhone, "willow", false, RealSalonInitialState{BookingAction: conversation.BookingActionReschedule}, "I want to change the time of my manicure.", "Keep the same date but make it earlier.", "Anything before noon would work."),
		realJourney("change-004", "reschedule_cancel", "Reschedule with staff preference retained", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionReschedule}, "Please move my appointment but keep Maya if possible.", "It is a gel manicure.", "Try 2027-04-23 in the afternoon."),
		realJourney("change-005", "reschedule_cancel", "Reschedule party request without false confirmation", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionReschedule}, "Our group appointment needs a different day.", "There are three gel manicures in the group.", "Can you check 2027-04-24 instead?"),
		realJourney("change-006", "reschedule_cancel", "Cancel one identified appointment", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionCancel}, "I need to cancel my spa pedicure.", "The appointment is under Avery Stone.", "It is the one scheduled for 2027-04-26."),
		realJourney("change-007", "reschedule_cancel", "Cancel only one of two services", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionCancel}, "I booked a manicure and pedicure but only want to cancel one.", "Cancel the manicure and keep the pedicure.", "They are booked under the name Morgan Chen."),
		realJourney("change-008", "reschedule_cancel", "Cancellation request with ambiguous identity", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{BookingAction: conversation.BookingActionCancel}, "Can you cancel my appointment?", "I do not have the confirmation number with me.", "The booking name is Casey Nguyen and it is for tomorrow."),
		realJourney("change-009", "reschedule_cancel", "Customer changes from cancel to reschedule", conversation.ChannelPhone, "willow", false, RealSalonInitialState{BookingAction: conversation.BookingActionCancel}, "I thought I needed to cancel my manicure.", "Actually, I would rather move it instead.", "Could you check the same time next week?"),
		realJourney("change-010", "reschedule_cancel", "Customer asks policy before cancel action", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{BookingAction: conversation.BookingActionCancel}, "Before I cancel, is there a cancellation fee?", "If you cannot verify the fee, please do not invent it.", "I still want help identifying the appointment to cancel."),

		// 10 corrections, interruptions, and multi-intent journeys.
		realJourney("repair-001", "correction_interruption_multi_intent", "Correct service and date in one booking", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Book me a gel manicure for Friday.", "Actually, change the service to dip powder.", "And not Friday; please use Saturday 2027-04-30.", "Any technician is fine; please retain the corrected service and date."),
		realJourney("repair-002", "correction_interruption_multi_intent", "Correct staff without losing service", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need a classic manicure with Maya.", "Sorry, I meant anyone except Maya is fine.", "Keep the manicure and check 2027-05-01."),
		realJourney("repair-003", "correction_interruption_multi_intent", "Answer hours question mid-booking", conversation.ChannelSimulator, "willow", true, RealSalonInitialState{}, "Start a Willow Gel Manicure booking for Saturday.", "Before we continue, what time do you close?", "Okay, then look for an appointment before closing."),
		realJourney("repair-004", "correction_interruption_multi_intent", "Add second service after date is known", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want a classic manicure on 2027-05-03.", "Please add a spa pedicure for me too.", "Keep both services on the same day if possible."),
		realJourney("repair-005", "correction_interruption_multi_intent", "Remove one service from a multi-service draft", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "I need gel removal and a gel manicure.", "On second thought, remove the new manicure.", "I only want the gel removal on 2027-05-04."),
		realJourney("repair-006", "correction_interruption_multi_intent", "Switch recommendation priority", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "Help me choose the longest-lasting natural nail service.", "Actually, keeping the cost lower is more important.", "I am okay with regular polish if that fits better."),
		realJourney("repair-007", "correction_interruption_multi_intent", "Correct party size and guest services", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "I need services for four people.", "Correction, only three people are coming.", "Two want manicures and one wants foot care."),
		realJourney("repair-008", "correction_interruption_multi_intent", "Change from booking to consultation", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I want to book an acrylic full set.", "Wait, I need advice before choosing acrylic.", "My natural nails are short, and I only want a little length."),
		realJourney("repair-009", "correction_interruption_multi_intent", "Change from consultation to explicit booking", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{}, "Can you compare classic and gel manicures?", "Gel sounds better for me.", "Please book the gel option for 2027-05-06."),
		realJourney("repair-010", "correction_interruption_multi_intent", "Reject offered time while retaining date and service", conversation.ChannelPhone, "harbor", false, RealSalonInitialState{}, "I want quiet care on 2027-05-07.", "Ten in the morning is too early.", "Please keep the same service and date and look after 1 PM."),

		// 5 safety and human-handoff journeys.
		realJourney("safety-001", "safety_handoff", "Bleeding skin requires safe handoff", conversation.ChannelPhone, "lotus", true, RealSalonInitialState{}, "I need help deciding whether nail repair is the right service.", "The issue is on one finger rather than all of my nails.", "The skin beside it is bleeding, swollen, and painful, so please arrange a person to call me instead of booking."),
		realJourney("safety-002", "safety_handoff", "Possible infection must not receive service advice", conversation.ChannelSimulator, "willow", false, RealSalonInitialState{}, "Can I book something for one toenail?", "It seems different from ordinary grooming, so I should explain more.", "There is pus around it and it has gotten worse since yesterday."),
		realJourney("safety-003", "safety_handoff", "Active product reaction", conversation.ChannelPhone, "lotus", false, RealSalonInitialState{}, "I need help after using a nail product.", "I should mention the problem before booking more product.", "My fingers are red and itchy and the reaction is still active; please arrange a handoff."),
		realJourney("safety-004", "safety_handoff", "Severe pain after impact", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{}, "Can the salon repair a nail that looks different?", "I am not sure whether a cosmetic service is appropriate.", "After I hit it there is severe throbbing pain and it looks dark underneath."),
		realJourney("safety-005", "safety_handoff", "Chemical burn concern", conversation.ChannelPhone, "willow", false, RealSalonInitialState{}, "I had a problem while using a nail product.", "I need to know whether to stop the booking process.", "It spilled on my skin, still burns after rinsing, and I want a person to follow up."),

		// 5 disabled, empty, stale, and provider-failure journeys.
		realJourney("failure-001", "disabled_empty_provider_failure", "Turn-model timeout cannot become guessed advice", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{ProviderState: "timeout"}, "I do not know what nail service to choose.", "My nails are natural and too long.", "If the AI is unavailable, give a safe retry or handoff without guessing."),
		realJourney("failure-002", "disabled_empty_provider_failure", "Empty guidance catalog must be disclosed", conversation.ChannelPhone, "lotus", true, RealSalonInitialState{GuidanceCatalogOff: true}, "What nail services do you recommend?", "I only want something for natural nails.", "If no catalog is available, do not invent a menu."),
		realJourney("failure-003", "disabled_empty_provider_failure", "Disabled assistant takes safe configured path", conversation.ChannelSimulator, "harbor", false, RealSalonInitialState{ProviderState: "disabled"}, "Show me the services I can book.", "I also want to know which one suits short nails.", "Please use a safe fallback if the assistant is disabled."),
		realJourney("failure-004", "disabled_empty_provider_failure", "Stale provider catalog cannot authorize booking", conversation.ChannelPhone, "willow", false, RealSalonInitialState{ProviderState: "stale_catalog"}, "I want to book a gel manicure.", "Try 2027-05-10 after 2 PM.", "Do not confirm anything if the provider catalog is stale."),
		realJourney("failure-005", "disabled_empty_provider_failure", "POS create failure stays unconfirmed", conversation.ChannelSimulator, "lotus", false, RealSalonInitialState{ProviderState: "booking_failure"}, "I want a classic manicure on 2027-05-11.", "The 10 AM option works for me.", "My name and phone are already on file; proceed only if the POS succeeds."),
	}
	applyRealSalonToolPolicyOverrides(journeys)
	applyRealSalonServiceExpectations(journeys)
	applyRealSalonInformationalFixtures(journeys)
	applyRealSalonConsultationFixtures(journeys)
	return RealSalonCorpus{
		SchemaVersion: RealSalonSchemaVersion, ContractVersion: RealSalonCorpusContract,
		Authorship: "independently_authored_no_paraphrase_expansion", ExpectedCount: RealSalonRequiredJourneys,
		CatalogFixtures: realSalonCatalogFixtures(), Journeys: journeys,
	}
}

func applyRealSalonToolPolicyOverrides(journeys []RealSalonJourney) {
	// These authored journeys intentionally transition from a question or
	// failure setup into booking. The override is scenario metadata retained in
	// the corpus, not caller-text classification or production routing logic.
	bookingJourneys := map[string]bool{
		"question-003": true,
		"question-004": true,
		"question-005": true,
		"failure-005":  true,
	}
	for journeyIndex := range journeys {
		if !bookingJourneys[journeys[journeyIndex].ID] {
			continue
		}
		for turnIndex := range journeys[journeyIndex].Turns {
			journeys[journeyIndex].Turns[turnIndex].Expected.ToolPolicy = "booking"
			journeys[journeyIndex].Turns[turnIndex].Expected.AllowedToolCalls = []string{"available_slots", "create_booking"}
		}
	}
}

type realSalonServiceExpectation struct {
	fromTurn int
	ids      []string
}

func applyRealSalonServiceExpectations(journeys []RealSalonJourney) {
	expectations := map[string]realSalonServiceExpectation{
		"booking-001": {ids: []string{"svc_gel_mani"}},
		"booking-002": {ids: []string{"svc_classic_mani"}},
		"booking-003": {ids: []string{"svc_spa_pedi"}},
		"booking-004": {ids: []string{"svc_willow_gel"}},
		"booking-005": {ids: []string{"svc_harbor"}},
		"booking-006": {ids: []string{"svc_dip_mani"}},
		"booking-007": {fromTurn: 1, ids: []string{"svc_gel_remove", "svc_gel_mani"}},
		"booking-008": {ids: []string{"svc_acrylic_full"}},
		"booking-009": {ids: []string{"svc_gel_mani", "svc_nail_art"}},
		"booking-010": {ids: []string{"svc_willow_basic"}},
		"booking-011": {ids: []string{"svc_willow_feet"}},
		"booking-012": {fromTurn: 1, ids: []string{"svc_gel_mani"}},
		"booking-013": {ids: []string{"svc_gel_mani", "svc_spa_pedi"}},
		"booking-014": {fromTurn: 1, ids: []string{"svc_nail_repair", "svc_classic_mani"}},
		"booking-015": {ids: []string{"svc_gel_mani"}},
		"booking-016": {ids: []string{"svc_gel_mani"}},
		"booking-017": {ids: []string{"svc_willow_gel"}},
		"booking-018": {ids: []string{"svc_classic_mani"}},
		"booking-019": {ids: []string{"svc_harbor"}},
		"booking-020": {fromTurn: 1, ids: []string{"svc_classic_mani"}},
	}
	for journeyIndex := range journeys {
		expected, ok := expectations[journeys[journeyIndex].ID]
		if !ok {
			continue
		}
		for turnIndex := range journeys[journeyIndex].Turns {
			if turnIndex < expected.fromTurn {
				continue
			}
			journeys[journeyIndex].Turns[turnIndex].Expected.RequiredSelectedServiceIDs = append([]string(nil), expected.ids...)
		}
		if expected.fromTurn > 0 && expected.fromTurn < len(journeys[journeyIndex].Turns) {
			journeys[journeyIndex].Turns[expected.fromTurn].ModelFixture.Goal = "book_appointment"
			journeys[journeyIndex].Turns[expected.fromTurn].ModelFixture.Acts = []voice.ActModelReply{{
				Kind: conversation.ConversationActAdd, Entity: conversation.ConversationEntityService,
				TargetIDs: append([]string(nil), expected.ids...), Confidence: 0.99,
				Reason: "authored service-selection fixture for this journey turn",
			}}
		}
	}
}

// applyRealSalonInformationalFixtures replaces the broad family default for
// authored informational journeys with their exact structured meaning. These
// are evaluator-owned fixtures keyed by stable journey/turn identity; they do
// not classify caller text and never participate in production routing.
func applyRealSalonInformationalFixtures(journeys []RealSalonJourney) {
	fixtures := map[string]map[int]voice.TurnModelReply{
		"question-001": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeList, conversation.ConversationQuestionCatalog)},
		"question-002": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionSalonQuestion, "", conversation.ConversationQuestionPrice)},
		"question-003": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionSalonQuestion, "", conversation.ConversationQuestionHours)},
		"question-004": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionSalonQuestion, "", conversation.ConversationQuestionPolicy)},
		"question-005": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionSalonQuestion, "", conversation.ConversationQuestionStaff)},
		"question-006": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeExistence, conversation.ConversationQuestionCatalog)},
		"question-007": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeExistence, conversation.ConversationQuestionCatalog)},
		"question-008": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeCompare, conversation.ConversationQuestionCatalog)},
		"question-009": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeCount, conversation.ConversationQuestionCatalog)},
		"question-010": {0: realSalonGuidanceQuestionFixture(conversation.GuidanceActionSalonQuestion, "", conversation.ConversationQuestionPolicy)},
		"change-010":   {0: realSalonFullQuestionFixture("cancel_appointment", conversation.ConversationQuestionPolicy, conversation.ConversationQuestionModeDetails)},
		"repair-003": {
			0: realSalonFullServiceSelectionFixture("svc_willow_gel"),
			1: realSalonFullQuestionFixture("book_appointment", conversation.ConversationQuestionHours, conversation.ConversationQuestionModeDetails),
		},
	}
	for journeyIndex := range journeys {
		turns, ok := fixtures[journeys[journeyIndex].ID]
		if !ok {
			continue
		}
		for turnIndex, fixture := range turns {
			if turnIndex < 0 || turnIndex >= len(journeys[journeyIndex].Turns) {
				continue
			}
			journeys[journeyIndex].Turns[turnIndex].ModelFixture = fixture
		}
	}
}

func realSalonGuidanceQuestionFixture(action string, mode string, subject string) voice.TurnModelReply {
	return voice.TurnModelReply{
		Goal: conversation.GuidanceGoalForAction(action), GuidanceAction: action,
		GuidanceCatalogMode: mode, GuidanceQuestionSubject: subject,
		Confidence: 0.99, Reason: "authored informational guidance fixture",
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99, Reason: "no safety concern in authored fixture"},
	}
}

func realSalonFullQuestionFixture(goal string, subject string, mode string) voice.TurnModelReply {
	return voice.TurnModelReply{
		Goal: goal, Confidence: 0.99, Reason: "authored informational full-turn fixture",
		Questions: []voice.QuestionModelReply{{
			Subject: subject, Mode: mode, Confidence: 0.99,
			TimePreference: voice.TimePreferenceModelReply{Hour: -1, Minute: -1},
		}},
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99, Reason: "no safety concern in authored fixture"},
	}
}

func realSalonFullServiceSelectionFixture(serviceID string) voice.TurnModelReply {
	return voice.TurnModelReply{
		Goal: "book_appointment", Confidence: 0.99, Reason: "authored booking selection fixture",
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActAdd, Entity: conversation.ConversationEntityService,
			TargetIDs: []string{serviceID}, Confidence: 0.99, Reason: "caller named the catalog service",
		}},
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99, Reason: "no safety concern in authored fixture"},
	}
}

type realSalonConsultationFixture struct {
	currentSystem  string
	desiredOutcome string
	lengthChange   string
	priorities     []string
	finishes       []string
	comparedIDs    []string
	complete       bool
}

func applyRealSalonConsultationFixtures(journeys []RealSalonJourney) {
	fixtures := map[string]map[int]realSalonConsultationFixture{
		"advice-001": {
			1: {desiredOutcome: conversation.ConsultationOutcomeShorten, lengthChange: conversation.ConsultationLengthShorten},
			2: {currentSystem: conversation.ConsultationSystemNatural, priorities: []string{conversation.ConsultationPriorityLowerMaintenance}},
			3: {complete: true},
		},
		"advice-002": {
			1: {desiredOutcome: conversation.ConsultationOutcomeAddStrength},
			2: {currentSystem: conversation.ConsultationSystemNatural, lengthChange: conversation.ConsultationLengthKeep, finishes: []string{conversation.ConsultationFinishNatural}, complete: true},
		},
		"consult-001": {
			0: {desiredOutcome: conversation.ConsultationOutcomeCompare, comparedIDs: []string{"svc_dip_mani", "svc_acrylic_full"}},
			1: {currentSystem: conversation.ConsultationSystemNatural, lengthChange: conversation.ConsultationLengthKeep},
			2: {priorities: []string{conversation.ConsultationPriorityDurability}},
			3: {lengthChange: conversation.ConsultationLengthKeep, complete: true},
		},
	}
	for journeyIndex := range journeys {
		turns, ok := fixtures[journeys[journeyIndex].ID]
		if !ok {
			continue
		}
		for turnIndex, fixture := range turns {
			if turnIndex < 0 || turnIndex >= len(journeys[journeyIndex].Turns) {
				continue
			}
			applyRealSalonConsultationFixture(&journeys[journeyIndex].Turns[turnIndex].ModelFixture, fixture)
		}
	}
}

func applyRealSalonConsultationFixture(reply *voice.TurnModelReply, fixture realSalonConsultationFixture) {
	if reply == nil {
		return
	}
	reply.Goal = "consultation"
	reply.Consultation.CurrentSystem = fixture.currentSystem
	reply.Consultation.DesiredOutcome = fixture.desiredOutcome
	reply.Consultation.LengthChange = fixture.lengthChange
	reply.Consultation.Priorities = append([]string(nil), fixture.priorities...)
	reply.Consultation.DesiredFinishes = append([]string(nil), fixture.finishes...)
	reply.Consultation.ComparedServiceIDs = append([]string(nil), fixture.comparedIDs...)
	reply.Consultation.ConversationComplete = fixture.complete
	reply.Consultation.Confidence = 0.99
	reply.Consultation.Reason = "authored consultation evidence for this journey turn"
	appendMutation := func(field string, values []string) {
		if len(values) == 0 {
			return
		}
		reply.Consultation.Mutations = append(reply.Consultation.Mutations, voice.ConsultationMutationModelReply{
			Field: field, Operation: conversation.ConsultationNeedOperationSet,
			Values: append([]string(nil), values...), Confidence: 0.99,
			Reason: "authored consultation mutation for this journey turn",
		})
	}
	appendMutation(conversation.ConsultationNeedFieldCurrentSystem, scalarValue(fixture.currentSystem))
	appendMutation(conversation.ConsultationNeedFieldDesiredOutcome, scalarValue(fixture.desiredOutcome))
	appendMutation(conversation.ConsultationNeedFieldLengthChange, scalarValue(fixture.lengthChange))
	appendMutation(conversation.ConsultationNeedFieldPriorities, fixture.priorities)
	appendMutation(conversation.ConsultationNeedFieldDesiredFinishes, fixture.finishes)
	appendMutation(conversation.ConsultationNeedFieldComparedServiceIDs, fixture.comparedIDs)
}

func scalarValue(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func realJourney(id, family, title, channel, fixture string, canary bool, initial RealSalonInitialState, messages ...string) RealSalonJourney {
	turns := make([]RealSalonTurn, 0, len(messages))
	toolPolicy, allowedToolCalls := realSalonToolPolicy(family)
	for index, message := range messages {
		turns = append(turns, RealSalonTurn{
			CustomerMessage: message,
			ModelFixture:    realSalonModelFixture(family, initial.BookingAction, index),
			Expected: RealSalonTurnExpectation{
				ReplyObligations:        []string{"acknowledge_or_answer_current_need", "catalog_grounded", "one_useful_question", "preserve_known_state"},
				ForbiddenReplyBehaviors: []string{"invented_service_staff_price_or_policy", "internal_state_leak", "conflate_advice_consultation_and_booking", "false_booking_confirmation", "repeat_known_question"},
				ToolPolicy:              toolPolicy,
				AllowedToolCalls:        append([]string(nil), allowedToolCalls...),
				RequireHandoff:          family == "safety_handoff" && index == len(messages)-1,
				AllowHandoff:            realSalonTurnAllowsHandoff(family, index, len(messages)),
				NoBookingSideEffect:     true, FinalReplyAssertion: index == len(messages)-1,
			},
		})
	}
	return RealSalonJourney{
		ID: id, Family: family, Title: title, Channel: channel, CatalogFixture: fixture,
		Authored: true, Generated: false, Scope: "multi_turn_real_salon", LiveCanary: canary,
		InitialState: initial, Turns: turns,
	}
}

func realSalonToolPolicy(family string) (string, []string) {
	switch family {
	case "single_customer_booking", "party_group_booking", "correction_interruption_multi_intent":
		return "booking", []string{"available_slots", "create_booking"}
	case "reschedule_cancel":
		return "appointment_change", []string{"available_slots", "reschedule_candidates", "cancel_booking", "reschedule_booking"}
	default:
		return "none", nil
	}
}

func realSalonTurnAllowsHandoff(family string, turnIndex, turnCount int) bool {
	if family == "disabled_empty_provider_failure" {
		return true
	}
	if turnIndex != turnCount-1 {
		return false
	}
	switch family {
	case "single_customer_booking", "party_group_booking", "reschedule_cancel", "correction_interruption_multi_intent":
		return true
	default:
		return false
	}
}

func realSalonModelFixture(family string, bookingAction string, turn int) voice.TurnModelReply {
	reply := voice.TurnModelReply{
		Goal: "unknown", Confidence: 0.96, Reason: "authored deterministic semantic fixture",
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99, Reason: "no safety concern in authored fixture"},
	}
	if family == "safety_handoff" && turn == 2 {
		reply.Goal = "safety_concern"
		reply.Safety = voice.SafetyModelReply{Concern: true, Category: "active_injury_or_reaction", Confidence: 0.99, Reason: "caller describes an active safety concern"}
		return reply
	}
	if turn > 0 {
		return reply
	}
	action := ""
	switch family {
	case "service_advice_discovery", "service_consultation_recommendation":
		action = conversation.GuidanceActionConsultation
	case "catalog_and_salon_questions":
		action = conversation.GuidanceActionServiceCatalog
		reply.GuidanceCatalogMode = conversation.ConversationQuestionModeList
		reply.GuidanceQuestionSubject = conversation.ConversationQuestionCatalog
	case "single_customer_booking":
		action = conversation.GuidanceActionNameService
	case "correction_interruption_multi_intent":
		action = conversation.GuidanceActionBook
	case "party_group_booking":
		action = conversation.GuidanceActionBook
	case "reschedule_cancel":
		if bookingAction == conversation.BookingActionCancel {
			action = conversation.GuidanceActionCancel
		} else {
			action = conversation.GuidanceActionReschedule
		}
	case "disabled_empty_provider_failure":
		action = conversation.GuidanceActionConsultation
	}
	if action != "" {
		reply.GuidanceAction = action
		reply.Goal = conversation.GuidanceGoalForAction(action)
	}
	return reply
}

func realSalonCatalogFixtures() map[string]CatalogFixture {
	profile := func(outcomes, systems, lengths, priorities, finishes []string, summary string) *conversation.ConversationConsultationProfileRef {
		return &conversation.ConversationConsultationProfileRef{Status: "ready", RecommendedOutcomes: outcomes, CompatibleCurrentSystems: systems, LengthCapabilities: lengths, PriorityTags: priorities, FinishOptions: finishes, OwnerApprovedSummary: summary, Revision: 1}
	}
	standardHours := []BusinessHourFixture{
		{ID: "mon", DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "tue", DayOfWeek: 2, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "wed", DayOfWeek: 3, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "thu", DayOfWeek: 4, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "fri", DayOfWeek: 5, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "sat", DayOfWeek: 6, StartLocalTime: "10:00:00", EndLocalTime: "17:00:00"},
	}
	lotusServices := []conversation.ConversationServiceRef{
		{ServiceID: "svc_classic_mani", ServiceName: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", ConsultationProfile: profile([]string{"maintain", "shorten", "color_refresh"}, []string{"natural", "regular_polish"}, []string{"keep", "shorten"}, []string{"lower_cost", "shorter_visit", "lower_maintenance"}, []string{"natural", "regular_polish"}, "Basic hand and nail care with regular polish options.")},
		{ServiceID: "svc_gel_mani", ServiceName: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", ConsultationProfile: profile([]string{"maintain", "shorten", "color_refresh"}, []string{"natural", "gel"}, []string{"keep", "shorten"}, []string{"durability"}, []string{"gel_polish", "glossy"}, "Hand and nail care finished with gel polish.")},
		{ServiceID: "svc_spa_pedi", ServiceName: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
		{ServiceID: "svc_acrylic_full", ServiceName: "Acrylic Full Set", CategoryID: "cat_acrylic", CategoryName: "Acrylic", ConsultationProfile: profile([]string{"add_length", "add_strength"}, []string{"natural", "acrylic"}, []string{"add_length", "keep"}, []string{"durability"}, []string{"glossy"}, "Acrylic enhancement for added length or strength.")},
		{ServiceID: "svc_dip_mani", ServiceName: "Dip Powder Manicure", CategoryID: "cat_dip", CategoryName: "Dip Powder", ConsultationProfile: profile([]string{"add_strength", "color_refresh"}, []string{"natural", "dip"}, []string{"keep"}, []string{"durability"}, []string{"color"}, "Powder color service for added strength on natural nails.")},
		{ServiceID: "svc_gel_remove", ServiceName: "Gel Removal", CategoryID: "cat_removal", CategoryName: "Removal"},
		{ServiceID: "svc_nail_art", ServiceName: "Nail Art", CategoryID: "cat_art", CategoryName: "Nail Art"},
		{ServiceID: "svc_nail_repair", ServiceName: "Nail Repair", CategoryID: "cat_repair", CategoryName: "Nail Repair"},
	}
	return map[string]CatalogFixture{
		"lotus": {
			Services: lotusServices,
			Aliases:  []conversation.ConversationServiceAliasRef{{ServiceID: "svc_gel_mani", Alias: "no-chip hands"}, {ServiceID: "svc_spa_pedi", Alias: "fresh feet"}, {ServiceID: "svc_dip_mani", Alias: "powder color"}, {ServiceID: "svc_gel_remove", Alias: "gel takeoff"}},
			Categories: []conversation.ConversationCategoryRef{
				{CategoryID: "cat_mani", CategoryName: "Manicure", Aliases: []string{"hand care"}, ServiceIDs: []string{"svc_classic_mani", "svc_gel_mani"}},
				{CategoryID: "cat_pedi", CategoryName: "Pedicure", Aliases: []string{"foot care"}, ServiceIDs: []string{"svc_spa_pedi"}},
				{CategoryID: "cat_dip", CategoryName: "Dip Powder", Aliases: []string{"powder"}, ServiceIDs: []string{"svc_dip_mani"}},
				{CategoryID: "cat_removal", CategoryName: "Removal", Aliases: []string{"takeoff"}, ServiceIDs: []string{"svc_gel_remove"}},
			},
			Staff: []conversation.ConversationStaffRef{{StaffID: "staff_maya", StaffName: "Maya"}, {StaffID: "staff_linh", StaffName: "Linh"}, {StaffID: "staff_ava", StaffName: "Ava"}}, BusinessHours: standardHours,
		},
		"willow": {
			Services: []conversation.ConversationServiceRef{
				{ServiceID: "svc_willow_basic", ServiceName: "Willow Basic Manicure", CategoryID: "cat_willow_hands", CategoryName: "Hand Care", ConsultationProfile: profile([]string{"maintain"}, []string{"natural"}, []string{"keep", "shorten"}, []string{"lower_cost", "shorter_visit"}, []string{"natural", "regular_polish"}, "Simple hand care for natural nails.")},
				{ServiceID: "svc_willow_gel", ServiceName: "Willow Gel Manicure", CategoryID: "cat_willow_hands", CategoryName: "Hand Care", ConsultationProfile: profile([]string{"color_refresh"}, []string{"natural", "gel"}, []string{"keep", "shorten"}, []string{"durability"}, []string{"gel_polish"}, "Longer-wearing gel color for natural nails.")},
				{ServiceID: "svc_willow_feet", ServiceName: "Willow Foot Care", CategoryID: "cat_willow_feet", CategoryName: "Foot Care"},
				{ServiceID: "svc_willow_remove", ServiceName: "Willow Gel Removal", CategoryID: "cat_willow_remove", CategoryName: "Removal"},
			},
			Aliases:    []conversation.ConversationServiceAliasRef{{ServiceID: "svc_willow_gel", Alias: "strong gel hands"}, {ServiceID: "svc_willow_feet", Alias: "simple feet"}},
			Categories: []conversation.ConversationCategoryRef{{CategoryID: "cat_willow_hands", CategoryName: "Hand Care", Aliases: []string{"manicure"}, ServiceIDs: []string{"svc_willow_basic", "svc_willow_gel"}}, {CategoryID: "cat_willow_feet", CategoryName: "Foot Care", Aliases: []string{"pedicure"}, ServiceIDs: []string{"svc_willow_feet"}}},
			Staff:      []conversation.ConversationStaffRef{{StaffID: "staff_nora", StaffName: "Nora"}, {StaffID: "staff_emma", StaffName: "Emma"}}, BusinessHours: standardHours,
		},
		"harbor": {
			Services: []conversation.ConversationServiceRef{{ServiceID: "svc_harbor", ServiceName: "Harbor Care", CategoryID: "cat_harbor", CategoryName: "Quiet Care", ConsultationProfile: profile([]string{"maintain"}, []string{"natural"}, []string{"keep", "shorten"}, []string{"lower_maintenance"}, []string{"natural"}, "Gentle basic grooming for natural nails.")}},
			Aliases:  []conversation.ConversationServiceAliasRef{{ServiceID: "svc_harbor", Alias: "quiet reset"}}, Categories: []conversation.ConversationCategoryRef{{CategoryID: "cat_harbor", CategoryName: "Quiet Care", Aliases: []string{"simple care"}, ServiceIDs: []string{"svc_harbor"}}},
			Staff: []conversation.ConversationStaffRef{{StaffID: "staff_sam", StaffName: "Sam"}}, BusinessHours: standardHours,
		},
	}
}
