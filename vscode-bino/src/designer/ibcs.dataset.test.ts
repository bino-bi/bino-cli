import { describe, it, expect } from 'vitest';
import { FILTER_OPERATORS, FILTER_GROUP_OPS, GROUPBY_AGG_FUNCTIONS, INDEX_FUNCTIONS } from './ibcs';

// Parity guard against schema drift: these must equal the enums in
// internal/schema/jsonschema/document.schema.json (dataSetFilterCondition.op,
// dataSetFilterGroup.op, dataSetAggregate.fn, dataSetIndexColumn.fn).
describe('DataSet transform enum parity with document.schema.json', () => {
    it('FILTER_OPERATORS matches dataSetFilterCondition.op', () => {
        expect([...FILTER_OPERATORS]).toEqual(
            ['equal', 'notEqual', 'gte', 'gt', 'lte', 'lt', 'in', 'notIn', 'regex']
        );
    });
    it('FILTER_GROUP_OPS matches dataSetFilterGroup.op', () => {
        expect([...FILTER_GROUP_OPS]).toEqual(['and', 'or']);
    });
    it('GROUPBY_AGG_FUNCTIONS matches dataSetAggregate.fn', () => {
        expect([...GROUPBY_AGG_FUNCTIONS]).toEqual(
            ['sum', 'avg', 'min', 'max', 'first', 'last', 'count', 'countDistinct']
        );
    });
    it('INDEX_FUNCTIONS matches dataSetIndexColumn.fn', () => {
        expect([...INDEX_FUNCTIONS]).toEqual(['hash', 'rowNumber', 'rank', 'denseRank']);
    });
});
