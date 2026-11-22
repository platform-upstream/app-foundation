// Module kids: the copy (same keys in de and en, placeholders kept), the takeover suggestion, the
// contact link and the photo size. Started by TestKidsLogic with the source path.
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';

const m = await import(pathToFileURL(process.argv[2]).href);

const de = Object.keys(m.kidsLanguages.de).sort();
const en = Object.keys(m.kidsLanguages.en).sort();
assert.deepEqual(en, de, 'de and en have the same keys');
for (const key of de) {
  const vars = (s) => (s.match(/\{\w+\}/g) ?? []).sort().join();
  assert.equal(vars(m.kidsLanguages.en[key]), vars(m.kidsLanguages.de[key]), `placeholders of ${key}`);
}
assert.equal(m.kidsText('en').adopt, 'Take over');
assert.equal(m.kidsText('de', { adoptLead: 'Burgen?' }).adoptLead, 'Burgen?');
assert.equal(m.fill('{n} Kinder {x}', { n: 3 }), '3 Kinder {x}');

const kids = [{ id: 'a', name: 'Noah Meier' }, { id: 'b', name: 'Lea' }, { id: 'c', name: 'Élise' }];
assert.equal(m.suggestTarget('lea ', kids, true), 'b', 'same name, case and spaces ignored');
assert.equal(m.suggestTarget('Noah', kids, true), 'a', 'same first name');
assert.equal(m.suggestTarget('Elise', kids, false), 'c', 'accents folded');
assert.equal(m.suggestTarget('Tim', kids, true), m.AS_NEW, 'a new kid where one may be added');
assert.equal(m.suggestTarget('Tim', kids, false), 'a', 'else the first kid');
assert.equal(m.suggestTarget('Tim', [], false), m.NOT_YET, 'else not yet');

assert.equal(m.contactHref('https://example.org/x'), 'https://example.org/x');
assert.equal(m.contactHref(' privacy@example.org '), 'mailto:privacy@example.org');
assert.equal(m.contactHref('Ask the operator'), undefined);
assert.equal(m.contactHref('http://example.org'), undefined);

assert.deepEqual(m.fitSize(4032, 3024), { width: 512, height: 384 });
assert.deepEqual(m.fitSize(300, 200), { width: 300, height: 200 }, 'never upscaled');
assert.deepEqual(m.fitSize(10000, 1), { width: 512, height: 1 }, 'never 0');
console.log('ok');
