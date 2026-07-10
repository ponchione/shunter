# Shunter At A Vertically Integrated Construction Materials Company

Status: thought experiment

Scope: possible long-term Shunter uses at a company like New Enterprise Stone
& Lime Co., Inc. (NESL), based only on public information available on
2026-07-10. This note does not assert NESL's internal architecture, systems,
problems, plans, or approval of Shunter.

## Central Thesis

There is a strong potential fit, but not as a replacement for ERP, dispatch,
telematics, maintenance, estimating, quality, or analytics systems.

The compelling role for Shunter would be:

> A live operational coordination layer across systems that each understand
> only one part of the business.

NESL publicly describes itself as vertically integrated from quarry through
production and construction, with aggregates, asphalt, ready-mix concrete,
logistics, and heavy-highway work spread across Pennsylvania and Western New
York. Its public location directory showed 52 locations when this note was
written.

Public sources:

- [NESL company overview](https://www.nesl.com/about/)
- [NESL locations](https://www.nesl.com/locations/)
- [NESL products](https://www.nesl.com/products/)
- [NESL ready-mixed concrete](https://www.nesl.com/products/ready-mixed-concrete/)
- [NESL aggregate operations](https://www.nesl.com/products/aggregate/)
- [NESL seasonal operations](https://www.nesl.com/news/2026-season-careers-at-nesl/)
- [Indexed IT Solutions Architect posting](https://hiring.cafe/job/it-solutions-architect-new-enterprise-stone-and-lime-new-enterprise-jl7xdg0zswa4p81z)
- [NESL LinkedIn company page](https://www.linkedin.com/company/new-enterprise-stone-%26-lime-co--inc-/)

The publicly indexed Solutions Architect responsibilities span sales, dispatch
and logistics, trucking and telematics, estimating and costing, maintenance
and repair, integrations, workflows, and analytics. Those boundaries are where
Shunter could be useful: the underlying systems may each know their portion
well while no one system owns cross-functional resolution.

## Flagship Concept: An Operations Exception Hub

The system would not replace any operational platform. It would recognize
situations requiring coordination across platforms and give everyone one live
workspace in which to resolve them.

A normal delivery could remain entirely inside the dispatch system. Shunter
would become involved when something stops being normal:

```text
Dispatch order
     |
     +-- truck is late
     +-- plant goes down
     +-- material is placed on hold
     +-- jobsite is not ready
     +-- delivery window changes
     `-- customer requests a change
              |
              v
      Shunter exception case
              |
       assign / acknowledge
       investigate / decide
       notify / escalate
       resolve / document
```

A concrete or asphalt delivery exception can touch dispatch, the producing
plant, truck location and status, the construction project or customer, sales,
quality control, maintenance, and management. Shunter would own the active
coordination state while the source applications remain systems of record.

### Shunter Responsibilities

An exception would have an authoritative current state:

- affected order, load, plant, truck, customer, and project
- exception type and severity
- current owner
- required response time
- decisions made
- people notified
- corrective action
- resolution and outcome

Different teams would receive live views:

- dispatch sees affected loads and available alternatives
- plant personnel see production-impacting problems
- maintenance sees equipment-related exceptions
- sales sees customer-impacting delays
- field teams see project-impacting material changes
- supervisors see aging, unowned, or repeatedly escalated cases

Shunter procedures would communicate with vendor APIs, email, Teams, or other
services. Reducers would record operational decisions transactionally.
Scheduled work would drive escalation timers. Event streams would handle
transient alerts.

Shunter should retain the operational case and current context, not every GPS
point, batch record, or ERP transaction.

## Candidate Operational Scenarios

### 1. Delivery Exception Coordination

This is probably the strongest business concept.

Ready-mix and asphalt are time-sensitive products. NESL publicly emphasizes
modern dispatch, reliable scheduling, quality control, and on-time delivery for
concrete operations.

Potential exception flows include:

- a delayed truck threatens a pour window
- a truck is assigned but has not departed
- plant capacity changes after orders have been scheduled
- a customer or jobsite is not ready
- a load is rejected or redirected
- mix or ticket details require clarification
- weather interrupts paving or placement
- an alternate plant or truck must be selected
- sales needs to communicate an updated commitment

Shunter could give the organization a common live resolution workflow without
becoming the dispatch engine.

Useful outcome measures would include:

- time to acknowledge an exception
- time to make a disposition
- avoidable truck idle time
- number of calls required to coordinate resolution
- missed delivery windows
- customer notification time
- recurring causes by plant, region, or process

### 2. Plant And Equipment Downtime Coordination

A failure at a quarry, asphalt plant, or concrete plant affects more than
maintenance:

```text
Equipment fault
    -> production capacity changes
    -> orders or projects are exposed
    -> dispatch needs alternatives
    -> sales/customer communication may be needed
    -> maintenance needs ownership, parts, and ETA
```

A Shunter application could correlate, but not replace, the maintenance work
order, plant status, order commitments, and dispatch plan.

Live views might include:

- open production-impacting failures
- affected customers and projects
- estimated return-to-service time
- required parts or outside vendors
- alternate production locations
- pending operational decisions
- repeated failures awaiting root-cause review

This would be most useful where coordination currently occurs through phone,
radio, spreadsheets, email, or messaging around otherwise capable systems.

### 3. Quality Hold And Release Workflow

NESL's public materials emphasize formal quality control and compliance with
state and federal specifications. A Shunter workflow could coordinate:

- a test result or quality observation
- the affected product, stockpile, mix, batch, or production interval
- the initial hold
- potentially affected orders or tickets
- investigation assignment
- disposition: release, rework, redirect, or reject
- required approvals
- customer or project notification
- final resolution

Shunter should coordinate this work while laboratory, ticketing, and compliance
systems remain the official records. It should not become the sole regulatory
record or an automated safety interlock without the appropriate controls and
validation.

### 4. Construction Daily-Plan Coordination

For heavy-highway and paving operations, a daily operational workspace could
bring together:

- planned crews
- equipment
- material source and quantity
- trucking plan
- lane or site availability
- production status
- weather impacts
- daily progress
- blockers and changes
- next-shift handoff

Field personnel could report a blocker once, and the relevant plant,
dispatcher, superintendent, and project staff would see the same update.

Shunter would be strongest as the current-day coordination system. Estimating,
project accounting, scheduling, and historical cost analysis should remain in
their specialized systems.

### 5. Sales-To-Operations Handoff

An accepted quote does not necessarily mean an order is operationally ready. A
handoff workflow could verify:

- customer and project details
- product and specification
- approved mix or material
- expected quantities
- delivery location and restrictions
- production source
- haul or trucking assumptions
- scheduling requirements
- credit or administrative holds
- responsible sales and operations contacts

Shunter could expose missing information and unresolved decisions before the
first truck is scheduled.

### 6. Seasonal Readiness

NESL states that paving is busiest from spring through late fall, while quarry
operations continue through most of the year and some work shifts toward
maintenance and training in winter.

That creates a recurring coordination workflow covering:

- plant readiness
- equipment inspection
- calibration
- staffing and training
- material and parts availability
- vendor readiness
- system configuration
- test orders
- outstanding defects
- go/no-go approval

A company-wide readiness view across many locations could be an excellent
bounded use case. It involves many participants and dependencies but carries
less operational risk than starting with live dispatch control.

### 7. Safety Corrective-Action Coordination

Shunter could help manage the work surrounding observations, near misses,
corrective actions, inspection findings, owners, deadlines, verification,
site-level visibility, and escalation of overdue action.

It should not operate machinery, enforce safety interlocks, or replace an
approved safety or compliance system. Its role would be ensuring that human
corrective work does not become invisible or fall between departments.

### 8. IT Rollout And Site-Readiness Management

This may be the safest way to dogfood the concept:

- application rollout by site
- network and device readiness
- user training
- data-conversion status
- user-acceptance findings
- go-live decision
- hypercare incidents
- vendor issues
- configuration differences
- acceptance and closeout

It would let IT experience Shunter's realtime coordination model on its own
work before proposing it for operational processes.

## Architectural Position

```text
ERP / Sales / Estimating ------+
Dispatch / Ticketing ----------+
Telematics --------------------+
Maintenance / CMMS ------------+---> Integration layer
Quality systems ---------------+           |
Project systems ---------------+           v
                                    Shunter operational state
                                      |      |       |
                                  live views |   timers/events
                                      |      |       |
                                      `-- Operations workspace
                                                 |
                                      approved actions/integrations
                                                 |
                                                 v
                                      Systems of record
```

The governing principle is:

> Systems of record know their respective business objects; Shunter knows
> which people and systems must coordinate around the current situation.

## Explicit Non-Uses

At a company like NESL, Shunter should not be positioned as:

- an ERP replacement
- the dispatch or concrete-batching engine
- a telematics history database
- a repository for raw high-frequency truck GPS data
- a CMMS replacement
- a data warehouse or enterprise BI platform
- a project estimating or job-costing engine
- a PLC, SCADA, or plant-control system
- a safety interlock
- the sole DOT or quality compliance record
- a mobile offline-first field platform

A raw telematics firehose would be a particularly poor fit. Shunter should
receive meaningful transitions such as truck delayed, geofence entered,
assignment changed, or vehicle unavailable rather than every coordinate
emitted by every vehicle.

## Suggested Discovery And Pilot Path

Look for a process with four characteristics:

1. It crosses at least two existing systems.
2. People coordinate it by phone, radio, spreadsheet, email, or messaging.
3. Delayed decisions have a measurable operational cost.
4. No existing system clearly owns the entire resolution flow.

Start read-only:

- observe events from existing systems
- create a shared live view
- let users acknowledge and annotate exceptions
- measure whether awareness and response improve

Only after that succeeds should the application write decisions back into
operational systems.

The best first pilot would likely be seasonal/site readiness or a narrowly
scoped delivery-exception workflow. The former has lower operational risk; the
latter has potentially much higher business value.

## Governance Note

Because Shunter predates the project owner's employment and is independently
developed, establish written boundaries before using it with employer data or
employer-funded development. Relevant questions include pre-existing
intellectual property, licensing, ownership of improvements, security
approval, data classification, support responsibility, and acceptable
experimentation.

Shunter would not be "the NESL database." Its potentially valuable role would
be the live connective tissue that helps a vertically integrated,
geographically distributed operation resolve cross-system problems as one
company.
